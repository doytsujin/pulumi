// *** WARNING: this file was generated by test. ***
// *** Do not edit by hand unless you're certain you know what you are doing! ***

import * as pulumi from "@pulumi/pulumi";
import * as inputs from "../types/input";
import * as outputs from "../types/output";
import * as enums from "../types/enums";

/**
 * Availability information of a product system.
 */
export interface AvailabilityInformationResponse {
    /**
     * Current availability stage of the product. Availability stage
     */
    availabilityStage: string;
    /**
     * Reason why the product is disabled.
     */
    disabledReason: string;
    /**
     * Message for why the product is disabled.
     */
    disabledReasonMessage: string;
}

/**
 * Holds billing meter details for each type of billing
 */
export interface BillingMeterDetailsResponse {
    /**
     * Frequency of recurrence
     */
    frequency: string;
    /**
     * Represents MeterDetails
     */
    meterDetails: outputs.Pav2MeterDetailsResponse | outputs.PurchaseMeterDetailsResponse;
    /**
     * Represents Metering type (eg one-time or recurrent)
     */
    meteringType: string;
    /**
     * Represents Billing type name
     */
    name: string;
}

/**
 * Configuration object.
 */
export interface ConfigurationResponse {
    /**
     * Availability information of the product system.
     */
    availabilityInformation: outputs.AvailabilityInformationResponse;
    /**
     * Cost information for the product system.
     */
    costInformation: outputs.CostInformationResponse;
    /**
     * Description related to the product system.
     */
    description: outputs.DescriptionResponse;
    /**
     * Dimensions of the configuration
     */
    dimensions: outputs.DimensionsResponse;
    /**
     * Display Name for the product system.
     */
    displayName: string;
    /**
     * list of filters supported for a product
     */
    filterableProperties: outputs.FilterablePropertyResponse[];
    /**
     * Hierarchy information of a product.
     */
    hierarchyInformation: outputs.HierarchyInformationResponse;
    /**
     * Image information for the product system.
     */
    imageInformation: outputs.ImageInformationResponse[];
    /**
     * Specifications of the configuration
     */
    specifications: outputs.SpecificationResponse[];
}

/**
 * Cost information for the product system
 */
export interface CostInformationResponse {
    /**
     * Default url to display billing information
     */
    billingInfoUrl: string;
    /**
     * Details on the various billing aspects for the product system.
     */
    billingMeterDetails: outputs.BillingMeterDetailsResponse[];
}

/**
 * Description related properties of a product system.
 */
export interface DescriptionResponse {
    /**
     * Attributes for the product system.
     */
    attributes: string[];
    /**
     * Type of description.
     */
    descriptionType: string;
    /**
     * Keywords for the product system.
     */
    keywords: string[];
    /**
     * Links for the product system.
     */
    links: outputs.LinkResponse[];
    /**
     * Long description of the product system.
     */
    longDescription: string;
    /**
     * Short description of the product system.
     */
    shortDescription: string;
}

/**
 * Dimensions of a configuration.
 */
export interface DimensionsResponse {
    /**
     * Depth of the device.
     */
    depth: number;
    /**
     * Height of the device.
     */
    height: number;
    /**
     * Length of the device.
     */
    length: number;
    /**
     * Unit for the dimensions of length, height and width.
     */
    lengthHeightUnit: string;
    /**
     * Weight of the device.
     */
    weight: number;
    /**
     * Unit for the dimensions of weight.
     */
    weightUnit: string;
    /**
     * Width of the device.
     */
    width: number;
}

/**
 * Different types of filters supported and its values.
 */
export interface FilterablePropertyResponse {
    /**
     * Values to be filtered.
     */
    supportedValues: string[];
    /**
     * Type of product filter.
     */
    type: string;
}

/**
 * Holds details about product hierarchy information
 */
export interface HierarchyInformationResponse {
    /**
     * Represents configuration name that uniquely identifies configuration
     */
    configurationName?: string;
    /**
     * Represents product family name that uniquely identifies product family
     */
    productFamilyName?: string;
    /**
     * Represents product line name that uniquely identifies product line
     */
    productLineName?: string;
    /**
     * Represents product name that uniquely identifies product
     */
    productName?: string;
}

/**
 * Image for the product
 */
export interface ImageInformationResponse {
    /**
     * Type of the image
     */
    imageType: string;
    /**
     * Url of the image
     */
    imageUrl: string;
}

/**
 * Returns link related to the product
 */
export interface LinkResponse {
    /**
     * Type of link
     */
    linkType: string;
    /**
     * Url of the link
     */
    linkUrl: string;
}

/**
 * Billing type PAV2 meter details
 */
export interface Pav2MeterDetailsResponse {
    /**
     * Represents billing type.
     * Expected value is 'Pav2'.
     */
    billingType: "Pav2";
    /**
     * Charging type.
     */
    chargingType: string;
    /**
     * Validation status of requested data center and transport.
     */
    meterGuid: string;
    /**
     * Billing unit applicable for Pav2 billing
     */
    multiplier: number;
}

/**
 * Product Family
 */
export interface ProductFamilyResponse {
    /**
     * Availability information of the product system.
     */
    availabilityInformation: outputs.AvailabilityInformationResponse;
    /**
     * Cost information for the product system.
     */
    costInformation: outputs.CostInformationResponse;
    /**
     * Description related to the product system.
     */
    description: outputs.DescriptionResponse;
    /**
     * Display Name for the product system.
     */
    displayName: string;
    /**
     * list of filters supported for a product
     */
    filterableProperties: outputs.FilterablePropertyResponse[];
    /**
     * Hierarchy information of a product.
     */
    hierarchyInformation: outputs.HierarchyInformationResponse;
    /**
     * Image information for the product system.
     */
    imageInformation: outputs.ImageInformationResponse[];
    /**
     * List of product lines supported in the product family
     */
    productLines: outputs.ProductLineResponse[];
}

/**
 * Product line
 */
export interface ProductLineResponse {
    /**
     * Availability information of the product system.
     */
    availabilityInformation: outputs.AvailabilityInformationResponse;
    /**
     * Cost information for the product system.
     */
    costInformation: outputs.CostInformationResponse;
    /**
     * Description related to the product system.
     */
    description: outputs.DescriptionResponse;
    /**
     * Display Name for the product system.
     */
    displayName: string;
    /**
     * list of filters supported for a product
     */
    filterableProperties: outputs.FilterablePropertyResponse[];
    /**
     * Hierarchy information of a product.
     */
    hierarchyInformation: outputs.HierarchyInformationResponse;
    /**
     * Image information for the product system.
     */
    imageInformation: outputs.ImageInformationResponse[];
    /**
     * List of products in the product line
     */
    products: outputs.ProductResponse[];
}

/**
 * List of Products
 */
export interface ProductResponse {
    /**
     * Availability information of the product system.
     */
    availabilityInformation: outputs.AvailabilityInformationResponse;
    /**
     * List of configurations for the product
     */
    configurations: outputs.ConfigurationResponse[];
    /**
     * Cost information for the product system.
     */
    costInformation: outputs.CostInformationResponse;
    /**
     * Description related to the product system.
     */
    description: outputs.DescriptionResponse;
    /**
     * Display Name for the product system.
     */
    displayName: string;
    /**
     * list of filters supported for a product
     */
    filterableProperties: outputs.FilterablePropertyResponse[];
    /**
     * Hierarchy information of a product.
     */
    hierarchyInformation: outputs.HierarchyInformationResponse;
    /**
     * Image information for the product system.
     */
    imageInformation: outputs.ImageInformationResponse[];
}

/**
 * Billing type Purchase meter details
 */
export interface PurchaseMeterDetailsResponse {
    /**
     * Represents billing type.
     * Expected value is 'Purchase'.
     */
    billingType: "Purchase";
    /**
     * Charging type.
     */
    chargingType: string;
    /**
     * Billing unit applicable for Pav2 billing
     */
    multiplier: number;
    /**
     * Product Id
     */
    productId: string;
    /**
     * Sku Id
     */
    skuId: string;
    /**
     * Term Id
     */
    termId: string;
}

/**
 * Specifications of the configurations
 */
export interface SpecificationResponse {
    /**
     * Name of the specification
     */
    name: string;
    /**
     * Value of the specification
     */
    value: string;
}

