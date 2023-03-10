// Copyright 2016-2018, Pulumi Corporation.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package display

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"runtime"
	"sort"
	"strings"
	"time"
	"unicode"

	"github.com/pulumi/pulumi/pkg/v3/backend/display/internal/terminal"
	"github.com/pulumi/pulumi/pkg/v3/engine"
	"github.com/pulumi/pulumi/pkg/v3/resource/deploy"
	"github.com/pulumi/pulumi/sdk/v3/go/common/apitype"
	"github.com/pulumi/pulumi/sdk/v3/go/common/diag"
	"github.com/pulumi/pulumi/sdk/v3/go/common/diag/colors"
	"github.com/pulumi/pulumi/sdk/v3/go/common/display"
	"github.com/pulumi/pulumi/sdk/v3/go/common/resource"
	"github.com/pulumi/pulumi/sdk/v3/go/common/tokens"
	"github.com/pulumi/pulumi/sdk/v3/go/common/util/cmdutil"
	"github.com/pulumi/pulumi/sdk/v3/go/common/util/contract"
)

// DiagInfo contains the bundle of diagnostic information for a single resource.
type DiagInfo struct {
	ErrorCount, WarningCount, InfoCount, DebugCount int

	// The very last diagnostic event we got for this resource (regardless of severity). We'll print
	// this out in the non-interactive mode whenever we get new events. Importantly, we don't want
	// to print out the most significant diagnostic, as that means a flurry of event swill cause us
	// to keep printing out the most significant diagnostic over and over again.
	LastDiag *engine.DiagEventPayload

	// The last error we received.  If we have an error, and we're in tree-view, we'll prefer to
	// show this over the last non-error diag so that users know about something bad early on.
	LastError *engine.DiagEventPayload

	// All the diagnostic events we've heard about this resource.  We'll print the last diagnostic
	// in the status region while a resource is in progress.  At the end we'll print out all
	// diagnostics for a resource.
	//
	// Diagnostic events are bucketed by their associated stream ID (with 0 being the default
	// stream).
	StreamIDToDiagPayloads map[int32][]engine.DiagEventPayload
}

type progressRenderer interface {
	io.Closer

	tick(display *ProgressDisplay)
	rowUpdated(display *ProgressDisplay, row Row)
	systemMessage(display *ProgressDisplay, payload engine.StdoutEventPayload)
	done(display *ProgressDisplay)
	println(display *ProgressDisplay, line string)
}

// ProgressDisplay organizes all the information needed for a dynamically updated "progress" view of an update.
type ProgressDisplay struct {
	opts Options

	renderer progressRenderer

	// action is the kind of action (preview, update, refresh, etc) being performed.
	action apitype.UpdateKind
	// stack is the stack this progress pertains to.
	stack tokens.Name
	// proj is the project this progress pertains to.
	proj tokens.PackageName

	// Whether or not we're previewing.  We don't know what we are actually doing until
	// we get the initial 'prelude' event.
	//
	// this flag is only used to adjust how we describe what's going on to the user.
	// i.e. if we're previewing we say things like "Would update" instead of "Updating".
	isPreview bool

	// The urn of the stack.
	stackUrn resource.URN

	// Whether or not we've seen outputs for the stack yet.
	seenStackOutputs bool

	// The summary event from the engine.  If we get this, we'll print this after all
	// normal resource events are heard.  That way we don't interfere with all the progress
	// messages we're outputting for them.
	summaryEventPayload *engine.SummaryEventPayload

	// Any system events we've received.  They will be printed at the bottom of all the status rows
	systemEventPayloads []engine.StdoutEventPayload

	// Used to record the order that rows are created in.  That way, when we present in a tree, we
	// can keep things ordered so they will not jump around.
	displayOrderCounter int

	// What tick we're currently on.  Used to determine the number of ellipses to concat to
	// a status message to help indicate that things are still working.
	currentTick int

	headerRow    Row
	resourceRows []ResourceRow

	// A mapping from each resource URN we are told about to its current status.
	eventUrnToResourceRow map[resource.URN]ResourceRow

	// Remember if we're a terminal or not.  In a terminal we get a little bit fancier.
	// For example, we'll go back and update previous status messages to make sure things
	// align.  We don't need to do that in non-terminal situations.
	isTerminal bool

	// If all progress messages are done and we can print out the final display.
	done bool

	// The column that the suffix should be added to
	suffixColumn int

	// the list of suffixes to rotate through
	suffixesArray []string

	// Maps used so we can generate short IDs for resource urns.
	urnToID map[resource.URN]string

	// Structure that tracks the time taken to perform an action on a resource.
	opStopwatch opStopwatch
}

type opStopwatch struct {
	start map[resource.URN]time.Time
	end   map[resource.URN]time.Time
}

func newOpStopwatch() opStopwatch {
	return opStopwatch{
		start: map[resource.URN]time.Time{},
		end:   map[resource.URN]time.Time{},
	}
}

// policyPayloads is a collection of policy violation events for a single resource.
var policyPayloads []engine.PolicyViolationEventPayload

func camelCase(s string) string {
	if len(s) == 0 {
		return s
	}

	runes := []rune(s)
	runes[0] = unicode.ToLower(runes[0])
	return string(runes)
}

func simplifyTypeName(typ tokens.Type) string {
	typeString := string(typ)

	components := strings.Split(typeString, ":")
	if len(components) != 3 {
		return typeString
	}
	pkg, module, name := components[0], components[1], components[2]

	if len(name) == 0 {
		return typeString
	}

	lastSlashInModule := strings.LastIndexByte(module, '/')
	if lastSlashInModule == -1 {
		return typeString
	}
	file := module[lastSlashInModule+1:]

	if file != camelCase(name) {
		return typeString
	}

	return fmt.Sprintf("%v:%v:%v", pkg, module[:lastSlashInModule], name)
}

// getEventUrn returns the resource URN associated with an event, or the empty URN if this is not an
// event that has a URN.  If this is also a 'step' event, then this will return the step metadata as
// well.
func getEventUrnAndMetadata(event engine.Event) (resource.URN, *engine.StepEventMetadata) {
	switch event.Type {
	case engine.ResourcePreEvent:
		payload := event.Payload().(engine.ResourcePreEventPayload)
		return payload.Metadata.URN, &payload.Metadata
	case engine.ResourceOutputsEvent:
		payload := event.Payload().(engine.ResourceOutputsEventPayload)
		return payload.Metadata.URN, &payload.Metadata
	case engine.ResourceOperationFailed:
		payload := event.Payload().(engine.ResourceOperationFailedPayload)
		return payload.Metadata.URN, &payload.Metadata
	case engine.DiagEvent:
		return event.Payload().(engine.DiagEventPayload).URN, nil
	case engine.PolicyViolationEvent:
		return event.Payload().(engine.PolicyViolationEventPayload).ResourceURN, nil
	default:
		return "", nil
	}
}

// ShowProgressEvents displays the engine events with docker's progress view.
func ShowProgressEvents(op string, action apitype.UpdateKind, stack tokens.Name, proj tokens.PackageName,
	permalink string, events <-chan engine.Event, done chan<- bool, opts Options, isPreview bool,
) {
	stdin := opts.Stdin
	if stdin == nil {
		stdin = os.Stdin
	}
	stdout := opts.Stdout
	if stdout == nil {
		stdout = os.Stdout
	}
	stderr := opts.Stderr
	if stderr == nil {
		stderr = os.Stderr
	}

	isInteractive, term := opts.IsInteractive, opts.term
	if isInteractive && term == nil {
		raw := runtime.GOOS != "windows"
		t, err := terminal.Open(stdin, stdout, raw)
		if err != nil {
			_, err = fmt.Fprintln(stderr, "Failed to open terminal; treating display as non-interactive (%w)", err)
			contract.IgnoreError(err)
			isInteractive = false
		} else {
			term = t
		}
	}

	var renderer progressRenderer
	if isInteractive {
		printOpenInBrowser(term, opts, permalink)
		renderer = newInteractiveRenderer(term, permalink, opts)
	} else {
		printViewLive(stdout, opts, permalink)
		renderer = newNonInteractiveRenderer(stdout, op, opts)
	}

	display := &ProgressDisplay{
		action:                action,
		isPreview:             isPreview,
		isTerminal:            isInteractive,
		opts:                  opts,
		renderer:              renderer,
		stack:                 stack,
		proj:                  proj,
		eventUrnToResourceRow: make(map[resource.URN]ResourceRow),
		suffixColumn:          int(statusColumn),
		suffixesArray:         []string{"", ".", "..", "..."},
		urnToID:               make(map[resource.URN]string),
		displayOrderCounter:   1,
		opStopwatch:           newOpStopwatch(),
	}

	ticker := time.NewTicker(1 * time.Second)
	if opts.deterministicOutput {
		ticker.Stop()
	}
	display.processEvents(ticker, events)
	contract.IgnoreClose(display.renderer)
	ticker.Stop()

	// let our caller know we're done.
	close(done)
}

func (display *ProgressDisplay) println(line string) {
	display.renderer.println(display, line)
}

type treeNode struct {
	row Row

	colorizedColumns []string
	colorizedSuffix  string

	childNodes []*treeNode
}

func (display *ProgressDisplay) getOrCreateTreeNode(
	result *[]*treeNode, urn resource.URN, row ResourceRow, urnToTreeNode map[resource.URN]*treeNode,
) *treeNode {
	node, has := urnToTreeNode[urn]
	if has {
		return node
	}

	node = &treeNode{
		row:              row,
		colorizedColumns: row.ColorizedColumns(),
		colorizedSuffix:  row.ColorizedSuffix(),
	}

	urnToTreeNode[urn] = node

	// if it's the not the root item, attach it as a child node to an appropriate parent item.
	if urn != "" && urn != display.stackUrn {
		var parentURN resource.URN

		res := row.Step().Res
		if res != nil {
			parentURN = res.Parent
		}

		parentRow, hasParentRow := display.eventUrnToResourceRow[parentURN]

		if !hasParentRow {
			// If we haven't heard about this node's parent, then  just parent it to the stack.
			// Note: getting the parent row for the stack-urn will always succeed as we ensure that
			// such a row is always there in ensureHeaderAndStackRows
			parentURN = display.stackUrn
			parentRow = display.eventUrnToResourceRow[parentURN]
		}

		parentNode := display.getOrCreateTreeNode(result, parentURN, parentRow, urnToTreeNode)
		parentNode.childNodes = append(parentNode.childNodes, node)
		return node
	}

	*result = append(*result, node)
	return node
}

func (display *ProgressDisplay) generateTreeNodes() []*treeNode {
	result := []*treeNode{}

	result = append(result, &treeNode{
		row:              display.headerRow,
		colorizedColumns: display.headerRow.ColorizedColumns(),
	})

	urnToTreeNode := make(map[resource.URN]*treeNode)
	for urn, row := range display.eventUrnToResourceRow {
		display.getOrCreateTreeNode(&result, urn, row, urnToTreeNode)
	}

	return result
}

func (display *ProgressDisplay) addIndentations(treeNodes []*treeNode, isRoot bool, indentation string) {
	childIndentation := indentation + "│  "
	lastChildIndentation := indentation + "   "

	for i, node := range treeNodes {
		isLast := i == len(treeNodes)-1

		prefix := indentation

		var nestedIndentation string
		if !isRoot {
			if isLast {
				prefix += "└─ "
				nestedIndentation = lastChildIndentation
			} else {
				prefix += "├─ "
				nestedIndentation = childIndentation
			}
		}

		node.colorizedColumns[typeColumn] = prefix + node.colorizedColumns[typeColumn]
		display.addIndentations(node.childNodes, false /*isRoot*/, nestedIndentation)
	}
}

func (display *ProgressDisplay) convertNodesToRows(
	nodes []*treeNode, maxSuffixLength int, rows *[][]string, maxColumnLengths *[]int,
) {
	for _, node := range nodes {
		if len(*maxColumnLengths) == 0 {
			*maxColumnLengths = make([]int, len(node.colorizedColumns))
		}

		colorizedColumns := make([]string, len(node.colorizedColumns))

		for i, colorizedColumn := range node.colorizedColumns {
			columnWidth := colors.MeasureColorizedString(colorizedColumn)

			if i == display.suffixColumn {
				columnWidth += maxSuffixLength
				colorizedColumns[i] = colorizedColumn + node.colorizedSuffix
			} else {
				colorizedColumns[i] = colorizedColumn
			}

			if columnWidth > (*maxColumnLengths)[i] {
				(*maxColumnLengths)[i] = columnWidth
			}
		}

		*rows = append(*rows, colorizedColumns)

		display.convertNodesToRows(node.childNodes, maxSuffixLength, rows, maxColumnLengths)
	}
}

type sortable []*treeNode

func (sortable sortable) Len() int {
	return len(sortable)
}

func (sortable sortable) Less(i, j int) bool {
	return sortable[i].row.DisplayOrderIndex() < sortable[j].row.DisplayOrderIndex()
}

func (sortable sortable) Swap(i, j int) {
	sortable[i], sortable[j] = sortable[j], sortable[i]
}

func sortNodes(nodes []*treeNode) {
	sort.Sort(sortable(nodes))

	for _, node := range nodes {
		childNodes := node.childNodes
		sortNodes(childNodes)
		node.childNodes = childNodes
	}
}

func (display *ProgressDisplay) filterOutUnnecessaryNodesAndSetDisplayTimes(nodes []*treeNode) []*treeNode {
	result := []*treeNode{}

	for _, node := range nodes {
		node.childNodes = display.filterOutUnnecessaryNodesAndSetDisplayTimes(node.childNodes)

		if node.row.HideRowIfUnnecessary() && len(node.childNodes) == 0 {
			continue
		}

		display.displayOrderCounter++
		node.row.SetDisplayOrderIndex(display.displayOrderCounter)
		result = append(result, node)
	}

	return result
}

func removeInfoColumnIfUnneeded(rows [][]string) {
	// If there have been no info messages, then don't print out the info column header.
	for i := 1; i < len(rows); i++ {
		row := rows[i]
		if row[len(row)-1] != "" {
			return
		}
	}

	firstRow := rows[0]
	firstRow[len(firstRow)-1] = ""
}

// Performs all the work at the end once we've heard about the last message from the engine.
// Specifically, this will update the status messages for any resources, and will also then
// print out all final diagnostics. and finally will print out the summary.
func (display *ProgressDisplay) processEndSteps() {
	// Figure out the rows that are currently in progress.
	var inProgressRows []ResourceRow
	if !display.isTerminal {
		for _, v := range display.eventUrnToResourceRow {
			if !v.IsDone() {
				inProgressRows = append(inProgressRows, v)
			}
		}
	}

	// Transition the display to the 'done' state.  This will transitively cause all
	// rows to become done.
	display.done = true

	// Now print out all those rows that were in progress.  They will now be 'done'
	// since the display was marked 'done'.
	if !display.isTerminal {
		for _, v := range inProgressRows {
			display.renderer.rowUpdated(display, v)
		}
	}

	// Now refresh everything.  This ensures that we go back and remove things like the diagnostic
	// messages from a status message (since we're going to print them all) below.  Note, this will
	// only do something in a terminal.  This is what we want, because if we're not in a terminal we
	// don't really want to reprint any finished items we've already printed.
	display.renderer.done(display)

	// Render several "sections" of output based on available data as applicable.
	display.println("")
	wroteDiagnosticHeader := display.printDiagnostics()
	wrotePolicyViolations := display.printPolicyViolations()
	display.printOutputs()
	// If no policies violated, print policy packs applied.
	if !wrotePolicyViolations {
		display.printSummary(wroteDiagnosticHeader)
	}
}

// printDiagnostics prints a new "Diagnostics:" section with all of the diagnostics grouped by
// resource. If no diagnostics were emitted, prints nothing.
func (display *ProgressDisplay) printDiagnostics() bool {
	// Since we display diagnostic information eagerly, we need to keep track of the first
	// time we wrote some output so we don't inadvertently print the header twice.
	wroteDiagnosticHeader := false
	for _, row := range display.eventUrnToResourceRow {
		// The header for the diagnogistics grouped by resource, e.g. "aws:apigateway:RestApi (accountsApi):"
		wroteResourceHeader := false

		// Each row in the display corresponded with a resource, and that resource could have emitted
		// diagnostics to various streams.
		for id, payloads := range row.DiagInfo().StreamIDToDiagPayloads {
			if len(payloads) == 0 {
				continue
			}

			if id != 0 {
				// For the non-default stream merge all the messages from the stream into a single
				// message.
				p := display.mergeStreamPayloadsToSinglePayload(payloads)
				payloads = []engine.DiagEventPayload{p}
			}

			// Did we write any diagnostic information for the resource x stream?
			wrote := false
			for _, v := range payloads {
				if v.Ephemeral {
					continue
				}

				msg := display.renderProgressDiagEvent(v, true /*includePrefix:*/)

				lines := splitIntoDisplayableLines(msg)
				if len(lines) == 0 {
					continue
				}

				// If we haven't printed the Diagnostics header, do so now.
				if !wroteDiagnosticHeader {
					wroteDiagnosticHeader = true
					display.println(colors.SpecHeadline + "Diagnostics:" + colors.Reset)
				}
				// If we haven't printed the header for the resource, do so now.
				if !wroteResourceHeader {
					wroteResourceHeader = true
					columns := row.ColorizedColumns()
					display.println(
						"  " + colors.BrightBlue + columns[typeColumn] + " (" + columns[nameColumn] + "):" + colors.Reset)
				}

				for _, line := range lines {
					line = strings.TrimRightFunc(line, unicode.IsSpace)
					display.println("    " + line)
				}

				wrote = true
			}

			if wrote {
				display.println("")
			}
		}

	}
	return wroteDiagnosticHeader
}

// printPolicyViolations prints a new "Policy Violation:" section with all of the violations
// grouped by policy pack. If no policy violations were encountered, prints nothing.
func (display *ProgressDisplay) printPolicyViolations() bool {
	// Loop through every resource and gather up all policy violations encountered.
	var policyEvents []engine.PolicyViolationEventPayload
	for _, row := range display.eventUrnToResourceRow {
		policyPayloads := row.PolicyPayloads()
		if len(policyPayloads) == 0 {
			continue
		}
		policyEvents = append(policyEvents, policyPayloads...)
	}
	if len(policyEvents) == 0 {
		return false
	}
	// Sort policy events by: policy pack name, policy pack version, enforcement level,
	// policy name, and finally the URN of the resource.
	sort.SliceStable(policyEvents, func(i, j int) bool {
		eventI, eventJ := policyEvents[i], policyEvents[j]
		if packNameCmp := strings.Compare(
			eventI.PolicyPackName,
			eventJ.PolicyPackName); packNameCmp != 0 {
			return packNameCmp < 0
		}
		if packVerCmp := strings.Compare(
			eventI.PolicyPackVersion,
			eventJ.PolicyPackVersion); packVerCmp != 0 {
			return packVerCmp < 0
		}
		if enfLevelCmp := strings.Compare(
			string(eventI.EnforcementLevel),
			string(eventJ.EnforcementLevel)); enfLevelCmp != 0 {
			return enfLevelCmp < 0
		}
		if policyNameCmp := strings.Compare(
			eventI.PolicyName,
			eventJ.PolicyName); policyNameCmp != 0 {
			return policyNameCmp < 0
		}
		urnCmp := strings.Compare(
			string(eventI.ResourceURN),
			string(eventJ.ResourceURN))
		return urnCmp < 0
	})

	// Print every policy violation, printing a new header when necessary.
	display.println(display.opts.Color.Colorize(colors.SpecHeadline + "Policy Violations:" + colors.Reset))

	for _, policyEvent := range policyEvents {
		// Print the individual policy event.
		c := colors.SpecImportant
		if policyEvent.EnforcementLevel == apitype.Mandatory {
			c = colors.SpecError
		}

		policyNameLine := fmt.Sprintf("    %s[%s]  %s v%s %s %s (%s: %s)",
			c, policyEvent.EnforcementLevel,
			policyEvent.PolicyPackName,
			policyEvent.PolicyPackVersion, colors.Reset,
			policyEvent.PolicyName,
			policyEvent.ResourceURN.Type(),
			policyEvent.ResourceURN.Name())
		display.println(policyNameLine)

		// The message may span multiple lines, so we massage it so it will be indented properly.
		message := strings.ReplaceAll(policyEvent.Message, "\n", "\n    ")
		messageLine := fmt.Sprintf("    %s", message)
		display.println(messageLine)
	}
	return true
}

// printOutputs prints the Stack's outputs for the display in a new section, if appropriate.
func (display *ProgressDisplay) printOutputs() {
	// Printing the stack's outputs wasn't desired.
	if display.opts.SuppressOutputs {
		return
	}
	// Cannot display outputs for the stack if we don't know its URN.
	if display.stackUrn == "" {
		return
	}

	stackStep := display.eventUrnToResourceRow[display.stackUrn].Step()

	props := getResourceOutputsPropertiesString(
		stackStep, 1, display.isPreview, display.opts.Debug,
		false /* refresh */, display.opts.ShowSameResources)
	if props != "" {
		display.println(colors.SpecHeadline + "Outputs:" + colors.Reset)
		display.println(props)
	}
}

// printSummary prints the Stack's SummaryEvent in a new section if applicable.
func (display *ProgressDisplay) printSummary(wroteDiagnosticHeader bool) {
	// If we never saw the SummaryEvent payload, we have nothing to do.
	if display.summaryEventPayload == nil {
		return
	}

	msg := renderSummaryEvent(*display.summaryEventPayload, wroteDiagnosticHeader, display.opts)
	display.println(msg)
}

func (display *ProgressDisplay) mergeStreamPayloadsToSinglePayload(
	payloads []engine.DiagEventPayload,
) engine.DiagEventPayload {
	buf := bytes.Buffer{}

	for _, p := range payloads {
		buf.WriteString(display.renderProgressDiagEvent(p, false /*includePrefix:*/))
	}

	firstPayload := payloads[0]
	msg := buf.String()
	return engine.DiagEventPayload{
		URN:       firstPayload.URN,
		Message:   msg,
		Prefix:    firstPayload.Prefix,
		Color:     firstPayload.Color,
		Severity:  firstPayload.Severity,
		StreamID:  firstPayload.StreamID,
		Ephemeral: firstPayload.Ephemeral,
	}
}

func splitIntoDisplayableLines(msg string) []string {
	lines := strings.Split(msg, "\n")

	// Trim off any trailing blank lines in the message.
	for len(lines) > 0 {
		lastLine := lines[len(lines)-1]
		if strings.TrimSpace(colors.Never.Colorize(lastLine)) == "" {
			lines = lines[0 : len(lines)-1]
		} else {
			break
		}
	}

	return lines
}

func (display *ProgressDisplay) processTick() {
	// Got a tick.  Update the progress display if we're in a terminal.  If we're not,
	// print a hearbeat message every 10 seconds after our last output so that the user
	// knows something is going on.  This is also helpful for hosts like jenkins that
	// often timeout a process if output is not seen in a while.
	display.currentTick++

	display.renderer.tick(display)
}

func (display *ProgressDisplay) getRowForURN(urn resource.URN, metadata *engine.StepEventMetadata) ResourceRow {
	// If there's already a row for this URN, return it.
	row, has := display.eventUrnToResourceRow[urn]
	if has {
		return row
	}

	// First time we're hearing about this resource. Create an initial nearly-empty status for it.
	step := engine.StepEventMetadata{URN: urn, Op: deploy.OpSame}
	if metadata != nil {
		step = *metadata
	}

	// If this is the first time we're seeing an event for the stack resource, check to see if we've already
	// recorded root events that we want to reassociate with this URN.
	if isRootURN(urn) {
		display.stackUrn = urn

		if row, has = display.eventUrnToResourceRow[""]; has {
			row.SetStep(step)
			display.eventUrnToResourceRow[urn] = row
			delete(display.eventUrnToResourceRow, "")
			return row
		}
	}

	row = &resourceRowData{
		display:              display,
		tick:                 display.currentTick,
		diagInfo:             &DiagInfo{},
		policyPayloads:       policyPayloads,
		step:                 step,
		hideRowIfUnnecessary: true,
	}

	display.eventUrnToResourceRow[urn] = row

	display.ensureHeaderAndStackRows()
	display.resourceRows = append(display.resourceRows, row)
	return row
}

func (display *ProgressDisplay) processNormalEvent(event engine.Event) {
	switch event.Type {
	case engine.PreludeEvent:
		// A prelude event can just be printed out directly to the console.
		// Note: we should probably make sure we don't get any prelude events
		// once we start hearing about actual resource events.
		payload := event.Payload().(engine.PreludeEventPayload)
		preludeEventString := renderPreludeEvent(payload, display.opts)
		if display.isTerminal {
			display.processNormalEvent(engine.NewEvent(engine.DiagEvent, engine.DiagEventPayload{
				Ephemeral: false,
				Severity:  diag.Info,
				Color:     cmdutil.GetGlobalColorization(),
				Message:   preludeEventString,
			}))
		} else {
			display.println(preludeEventString)
		}
		return
	case engine.SummaryEvent:
		// keep track of the summary event so that we can display it after all other
		// resource-related events we receive.
		payload := event.Payload().(engine.SummaryEventPayload)
		display.summaryEventPayload = &payload
		return
	case engine.DiagEvent:
		msg := display.renderProgressDiagEvent(event.Payload().(engine.DiagEventPayload), true /*includePrefix:*/)
		if msg == "" {
			return
		}
	case engine.StdoutColorEvent:
		display.handleSystemEvent(event.Payload().(engine.StdoutEventPayload))
		return
	}

	// At this point, all events should relate to resources.
	eventUrn, metadata := getEventUrnAndMetadata(event)

	// If we're suppressing reads from the tree-view, then convert notifications about reads into
	// ephemeral messages that will go into the info column.
	if metadata != nil && !display.opts.ShowReads {
		if metadata.Op == deploy.OpReadDiscard || metadata.Op == deploy.OpReadReplacement {
			// just flat out ignore read discards/replace.  They're only relevant in the context of
			// 'reads', and we only present reads as an ephemeral diagnostic anyways.
			return
		}

		if metadata.Op == deploy.OpRead {
			// Don't show reads as operations on a specific resource.  It's an underlying detail
			// that we don't want to clutter up the display with.  However, to help users know
			// what's going on, we can show them as ephemeral diagnostic messages that are
			// associated at the top level with the stack.  That way if things are taking a while,
			// there's insight in the display as to what's going on.
			display.processNormalEvent(engine.NewEvent(engine.DiagEvent, engine.DiagEventPayload{
				Ephemeral: true,
				Severity:  diag.Info,
				Color:     cmdutil.GetGlobalColorization(),
				Message:   fmt.Sprintf("read %v %v", simplifyTypeName(eventUrn.Type()), eventUrn.Name()),
			}))
			return
		}
	}

	if eventUrn == "" {
		// If this event has no URN, associate it with the stack. Note that there may not yet be a stack resource, in
		// which case this is a no-op.
		eventUrn = display.stackUrn
	}
	isRootEvent := eventUrn == display.stackUrn

	row := display.getRowForURN(eventUrn, metadata)

	// Don't bother showing certain events (for example, things that are unchanged). However
	// always show the root 'stack' resource so we can indicate that it's still running, and
	// also so we have something to attach unparented diagnostic events to.
	hideRowIfUnnecessary := metadata != nil && !shouldShow(*metadata, display.opts) && !isRootEvent
	// Always show row if there's a policy violation event. Policy violations prevent resource
	// registration, so if we don't show the row, the violation gets attributed to the stack
	// resource rather than the resources whose policy failed.
	hideRowIfUnnecessary = hideRowIfUnnecessary || event.Type == engine.PolicyViolationEvent
	if !hideRowIfUnnecessary {
		row.SetHideRowIfUnnecessary(false)
	}

	if event.Type == engine.ResourcePreEvent {
		step := event.Payload().(engine.ResourcePreEventPayload).Metadata

		// Register the resource update start time to calculate duration
		// and time elapsed.
		display.opStopwatch.start[step.URN] = time.Now()

		// Clear out potential event end timings for prior operations on the same resource.
		delete(display.opStopwatch.end, step.URN)

		row.SetStep(step)
	} else if event.Type == engine.ResourceOutputsEvent {
		isRefresh := display.getStepOp(row.Step()) == deploy.OpRefresh
		step := event.Payload().(engine.ResourceOutputsEventPayload).Metadata

		// Register the resource update end time to calculate duration
		// to display.
		display.opStopwatch.end[step.URN] = time.Now()

		// Is this the stack outputs event? If so, we'll need to print it out at the end of the plan.
		if step.URN == display.stackUrn {
			display.seenStackOutputs = true
		}

		row.SetStep(step)
		row.AddOutputStep(step)

		// If we're not in a terminal, we may not want to display this row again: if we're displaying a preview or if
		// this step is a no-op for a custom resource, refreshing this row will simply duplicate its earlier output.
		hasMeaningfulOutput := isRefresh ||
			!display.isPreview && (step.Res == nil || step.Res.Custom && step.Op != deploy.OpSame)
		if !display.isTerminal && !hasMeaningfulOutput {
			return
		}
	} else if event.Type == engine.ResourceOperationFailed {
		row.SetFailed()
	} else if event.Type == engine.DiagEvent {
		// also record this diagnostic so we print it at the end.
		row.RecordDiagEvent(event)
	} else if event.Type == engine.PolicyViolationEvent {
		// also record this policy violation so we print it at the end.
		row.RecordPolicyViolationEvent(event)
	} else {
		contract.Failf("Unhandled event type '%s'", event.Type)
	}

	display.renderer.rowUpdated(display, row)
}

func (display *ProgressDisplay) handleSystemEvent(payload engine.StdoutEventPayload) {
	// Make sure we have a header to display
	display.ensureHeaderAndStackRows()

	display.systemEventPayloads = append(display.systemEventPayloads, payload)

	display.renderer.systemMessage(display, payload)
}

func (display *ProgressDisplay) ensureHeaderAndStackRows() {
	if display.headerRow == nil {
		// about to make our first status message.  make sure we present the header line first.
		display.headerRow = &headerRowData{display: display}
	}

	// we've added at least one row to the table.  make sure we have a row to designate the
	// stack if we haven't already heard about it yet.  This also ensures that as we build
	// the tree we can always guarantee there's a 'root' to parent anything to.
	_, hasStackRow := display.eventUrnToResourceRow[display.stackUrn]
	if hasStackRow {
		return
	}

	stackRow := &resourceRowData{
		display:              display,
		tick:                 display.currentTick,
		diagInfo:             &DiagInfo{},
		policyPayloads:       policyPayloads,
		step:                 engine.StepEventMetadata{Op: deploy.OpSame},
		hideRowIfUnnecessary: false,
	}

	display.eventUrnToResourceRow[display.stackUrn] = stackRow
	display.resourceRows = append(display.resourceRows, stackRow)
}

func (display *ProgressDisplay) processEvents(ticker *time.Ticker, events <-chan engine.Event) {
	// Main processing loop.  The purpose of this func is to read in events from the engine
	// and translate them into Status objects and progress messages to be presented to the
	// command line.
	for {
		select {
		case <-ticker.C:
			display.processTick()

		case event := <-events:
			if event.Type == "" || event.Type == engine.CancelEvent {
				// Engine finished sending events.  Do all the final processing and return
				// from this local func.  This will print out things like full diagnostic
				// events, as well as the summary event from the engine.
				display.processEndSteps()
				return
			}

			display.processNormalEvent(event)
		}
	}
}

func (display *ProgressDisplay) renderProgressDiagEvent(payload engine.DiagEventPayload, includePrefix bool) string {
	if payload.Severity == diag.Debug && !display.opts.Debug {
		return ""
	}

	msg := payload.Message
	if includePrefix {
		msg = payload.Prefix + msg
	}

	return strings.TrimRightFunc(msg, unicode.IsSpace)
}

func (display *ProgressDisplay) getStepDoneDescription(step engine.StepEventMetadata, failed bool) string {
	makeError := func(v string) string {
		return colors.SpecError + "**" + v + "**" + colors.Reset
	}

	op := display.getStepOp(step)

	if display.isPreview {
		// During a preview, when we transition to done, we'll print out summary text describing the step instead of a
		// past-tense verb describing the step that was performed.
		return deploy.Color(op) + display.getPreviewDoneText(step) + colors.Reset
	}

	getDescription := func() string {
		opText := ""
		if failed {
			switch op {
			case deploy.OpSame:
				opText = "failed"
			case deploy.OpCreate, deploy.OpCreateReplacement:
				opText = "creating failed"
			case deploy.OpUpdate:
				opText = "updating failed"
			case deploy.OpDelete, deploy.OpDeleteReplaced:
				opText = "deleting failed"
			case deploy.OpReplace:
				opText = "replacing failed"
			case deploy.OpRead, deploy.OpReadReplacement:
				opText = "reading failed"
			case deploy.OpRefresh:
				opText = "refreshing failed"
			case deploy.OpReadDiscard, deploy.OpDiscardReplaced:
				opText = "discarding failed"
			case deploy.OpImport, deploy.OpImportReplacement:
				opText = "importing failed"
			default:
				contract.Failf("Unrecognized resource step op: %v", op)
				return ""
			}
		} else {
			switch op {
			case deploy.OpSame:
				opText = ""
			case deploy.OpCreate:
				opText = "created"
			case deploy.OpUpdate:
				opText = "updated"
			case deploy.OpDelete:
				opText = "deleted"
			case deploy.OpReplace:
				opText = "replaced"
			case deploy.OpCreateReplacement:
				opText = "created replacement"
			case deploy.OpDeleteReplaced:
				opText = "deleted original"
			case deploy.OpRead:
				opText = "read"
			case deploy.OpReadReplacement:
				opText = "read for replacement"
			case deploy.OpRefresh:
				opText = "refresh"
			case deploy.OpReadDiscard:
				opText = "discarded"
			case deploy.OpDiscardReplaced:
				opText = "discarded original"
			case deploy.OpImport:
				opText = "imported"
			case deploy.OpImportReplacement:
				opText = "imported replacement"
			default:
				contract.Failf("Unrecognized resource step op: %v", op)
				return ""
			}
		}
		if op == deploy.OpSame || display.opts.deterministicOutput || display.opts.SuppressTimings {
			return opText
		}

		start, ok := display.opStopwatch.start[step.URN]
		if !ok {
			return opText
		}

		end, ok := display.opStopwatch.end[step.URN]
		if !ok {
			return opText
		}

		opDuration := end.Sub(start).Seconds()
		if opDuration < 1 {
			// Display a more fine-grain duration as the operation
			// has completed.
			return fmt.Sprintf("%s (%.2fs)", opText, opDuration)
		}
		return fmt.Sprintf("%s (%ds)", opText, int(opDuration))
	}

	if failed {
		return makeError(getDescription())
	}

	return deploy.Color(op) + getDescription() + colors.Reset
}

func (display *ProgressDisplay) getPreviewText(step engine.StepEventMetadata) string {
	switch step.Op {
	case deploy.OpSame:
		return ""
	case deploy.OpCreate:
		return "create"
	case deploy.OpUpdate:
		return "update"
	case deploy.OpDelete:
		return "delete"
	case deploy.OpReplace:
		return "replace"
	case deploy.OpCreateReplacement:
		return "create replacement"
	case deploy.OpDeleteReplaced:
		return "delete original"
	case deploy.OpRead:
		return "read"
	case deploy.OpReadReplacement:
		return "read for replacement"
	case deploy.OpRefresh:
		return "refreshing"
	case deploy.OpReadDiscard:
		return "discard"
	case deploy.OpDiscardReplaced:
		return "discard original"
	case deploy.OpImport:
		return "import"
	case deploy.OpImportReplacement:
		return "import replacement"
	}

	contract.Failf("Unrecognized resource step op: %v", step.Op)
	return ""
}

// getPreviewDoneText returns a textual representation for this step, suitable for display during a preview once the
// preview has completed.
func (display *ProgressDisplay) getPreviewDoneText(step engine.StepEventMetadata) string {
	switch step.Op {
	case deploy.OpSame:
		return ""
	case deploy.OpCreate:
		return "create"
	case deploy.OpUpdate:
		return "update"
	case deploy.OpDelete:
		return "delete"
	case deploy.OpReplace, deploy.OpCreateReplacement, deploy.OpDeleteReplaced, deploy.OpReadReplacement,
		deploy.OpDiscardReplaced:
		return "replace"
	case deploy.OpRead:
		return "read"
	case deploy.OpRefresh:
		return "refresh"
	case deploy.OpReadDiscard:
		return "discard"
	case deploy.OpImport, deploy.OpImportReplacement:
		return "import"
	}

	contract.Failf("Unrecognized resource step op: %v", step.Op)
	return ""
}

func (display *ProgressDisplay) getStepOp(step engine.StepEventMetadata) display.StepOp {
	op := step.Op

	// We will commonly hear about replacements as an actual series of steps.  i.e. 'create
	// replacement', 'replace', 'delete original'.  During the actual application of these steps we
	// want to see these individual steps.  However, both before we apply all of them, and after
	// they're all done, we want to show this as a single conceptual 'replace'/'replaced' step.
	//
	// Note: in non-interactive mode we can show these all as individual steps.  This only applies
	// to interactive mode, where there is only one line shown per resource, and we want it to be as
	// clear as possible
	if display.isTerminal {
		// During preview, show the steps for replacing as a single 'replace' plan.
		// Once done, show the steps for replacing as a single 'replaced' step.
		// During update, we'll show these individual steps.
		if display.isPreview || display.done {
			if op == deploy.OpCreateReplacement || op == deploy.OpDeleteReplaced || op == deploy.OpDiscardReplaced {
				return deploy.OpReplace
			}
		}
	}

	return op
}

func (display *ProgressDisplay) getStepOpLabel(step engine.StepEventMetadata, done bool) string {
	return deploy.Prefix(display.getStepOp(step), done) + colors.Reset
}

func (display *ProgressDisplay) getStepInProgressDescription(step engine.StepEventMetadata) string {
	op := display.getStepOp(step)

	if isRootStack(step) && op == deploy.OpSame {
		// most of the time a stack is unchanged.  in that case we just show it as "running->done".
		// otherwise, we show what is actually happening to it.
		return "running"
	}

	getDescription := func() string {
		if display.isPreview {
			return display.getPreviewText(step)
		}

		opText := ""
		switch op {
		case deploy.OpSame:
			opText = ""
		case deploy.OpCreate:
			opText = "creating"
		case deploy.OpUpdate:
			opText = "updating"
		case deploy.OpDelete:
			opText = "deleting"
		case deploy.OpReplace:
			opText = "replacing"
		case deploy.OpCreateReplacement:
			opText = "creating replacement"
		case deploy.OpDeleteReplaced:
			opText = "deleting original"
		case deploy.OpRead:
			opText = "reading"
		case deploy.OpReadReplacement:
			opText = "reading for replacement"
		case deploy.OpRefresh:
			opText = "refreshing"
		case deploy.OpReadDiscard:
			opText = "discarding"
		case deploy.OpDiscardReplaced:
			opText = "discarding original"
		case deploy.OpImport:
			opText = "importing"
		case deploy.OpImportReplacement:
			opText = "importing replacement"
		default:
			contract.Failf("Unrecognized resource step op: %v", op)
			return ""
		}

		if op == deploy.OpSame || display.opts.deterministicOutput || display.opts.SuppressTimings {
			return opText
		}

		// Calculate operation time elapsed.
		start, ok := display.opStopwatch.start[step.URN]
		if !ok {
			return opText
		}

		secondsElapsed := time.Since(start).Seconds()
		return fmt.Sprintf("%s (%ds)", opText, int(secondsElapsed))
	}
	return deploy.ColorProgress(op) + getDescription() + colors.Reset
}
