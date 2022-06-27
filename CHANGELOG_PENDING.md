### Improvements

- [sdk/go] Added `PreviewDigest` for third party tools to be able to ingest the preview json
  [#9886](https://github.com/pulumi/pulumi/pull/9886)

### Bug Fixes

- [engine] Filter out non-targeted resources much earlier in the engine cycle.
  [#9960](https://github.com/pulumi/pulumi/pull/9960)

- [cli] `pulumi convert` supports provider packages without a version.
  [#9976](https://github.com/pulumi/pulumi/pull/9976)
