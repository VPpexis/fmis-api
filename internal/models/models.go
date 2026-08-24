// Package models contains the domain struct and enums type mirroring the database schema.
package models

import (
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
)

// ProductType classifies a product in the catalog.
type ProductType string

// BatchStatusType describes the lifecycle state of an inventory batch.
type BatchStatusType string

// TransactionType classifies a transaction in the catalog.
type TransactionType string

// ProductionOrderStatusType classifies production_order_status in the catalog.
type ProductionOrderStatusType string

// UserRoleType defines the access level of a user account
type UserRoleType string

// ProductType values classify a product in the catalog.
const (
	ProductTypeRawMaterial  ProductType = "RAW_MATERIAL"
	ProductTypePackaging    ProductType = "PACKAGING"
	ProductTypeFinishedGood ProductType = "FINISHED_GOOD"
	ProductTypeWhiteLabel   ProductType = "WHITE_LABEL"
)

// BatchStatusType values describe the lifecycle state of an inventory batch.
const (
	BatchStatusTypeActive      BatchStatusType = "ACTIVE"
	BatchStatusTypeDepleted    BatchStatusType = "DEPLETED"
	BatchStatusTypeQuarantined BatchStatusType = "QUARANTINED"
	BatchStatusTypeExpired     BatchStatusType = "EXPIRED"
	BatchStatusTypeReserved    BatchStatusType = "RESERVED"
)

// TransactionType values describe the kind of movement recorded on a batch.
const (
	TransactionTypeIncoming         TransactionType = "INCOMING"
	TransactionTypeOutgoing         TransactionType = "OUTGOING"
	TransactionTypeUsedInProduction TransactionType = "USED_IN_PRODUCTION"
	TransactionTypeWaste            TransactionType = "WASTE"
	TransactionTypeAdjustment       TransactionType = "ADJUSTMENT"
)

// ProductionOrderStatusType values track a production order's lifecycle.
const (
	ProductionOrderStatusTypePlanned    ProductionOrderStatusType = "PLANNED"
	ProductionOrderStatusTypeInProgress ProductionOrderStatusType = "IN_PROGRESS"
	ProductionOrderStatusTypeCompleted  ProductionOrderStatusType = "COMPLETED"
	ProductionOrderStatusTypeCancelled  ProductionOrderStatusType = "CANCELLED"
)

// UserRoleType values define the access level of a user account.
const (
	UserRoleTypeAdmin    UserRoleType = "ADMIN"
	UserRoleTypeOperator UserRoleType = "OPERATOR"
	UserRoleTypeViewer   UserRoleType = "VIEWER"
)

// Product is a catalog item that can be purchased, manufactured, or sold.
type Product struct {
	ID            uuid.UUID   `json:"id"`
	SKU           string      `json:"sku"`
	Name          string      `json:"name"`
	UnitOfMeasure string      `json:"unit_of_measure"`
	ProductType   ProductType `json:"product_type"`
	IsPurchasable bool        `json:"is_purchasable"`
	IsSellable    bool        `json:"is_sellable"`
	CreatedAt     time.Time   `json:"created_at"`
	UpdatedAt     time.Time   `json:"updated_at"`
}

// InventoryBatch is a resolved lot of a product tracked for FEFO consumption.
type InventoryBatch struct {
	ID              uuid.UUID          `json:"id"`
	ProductID       uuid.UUID          `json:"product_id"`
	BatchNumber     string             `json:"batch_number"`
	QuantityInitial pgtype.Numeric     `json:"quantity_initial"`
	QuantityCurrent pgtype.Numeric     `json:"quantity_current"`
	Status          BatchStatusType    `json:"status"`
	ExpirationDate  pgtype.Timestamptz `json:"expiration_date"`
	CreatedAt       time.Time          `json:"created_at"`
	UpdatedAt       time.Time          `json:"updated_at"`
}

// StockTransaction is an immutable audit entry recording a movement of stock.
type StockTransaction struct {
	ID                uuid.UUID       `json:"id"`
	BatchID           uuid.UUID       `json:"batch_id"`
	ProductionOrderID pgtype.UUID     `json:"production_order_id"`
	QuantityChange    pgtype.Numeric  `json:"quantity_change"`
	TransactionType   TransactionType `json:"transaction_type"`
	PerformedBy       uuid.UUID       `json:"performed_by"`
	ReferenceNote     pgtype.Text     `json:"reference_note"`
	CreatedAt         time.Time       `json:"created_at"`
}

// ProductionOrder assembles input batches info a finished-goods output batch.
type ProductionOrder struct {
	ID            uuid.UUID                 `json:"id"`
	OutputBatchID uuid.UUID                 `json:"output_batch_id"`
	Status        ProductionOrderStatusType `json:"status"`
	CreatedBy     uuid.UUID                 `json:"created_by"`
	CreatedAt     time.Time                 `json:"created_at"`
	CompletedAt   pgtype.Timestamptz        `json:"completed_at"`
}

// ProductionOrderLineItem records one input batch and its consumed quantity in a production order.
type ProductionOrderLineItem struct {
	ID                uuid.UUID      `json:"id"`
	ProductionOrderID uuid.UUID      `json:"production_order_id"`
	InputBatchID      uuid.UUID      `json:"input_batch_id"`
	QuantityConsumed  pgtype.Numeric `json:"quantity_consumed"`
	CreatedAt         time.Time      `json:"created_at"`
}

// User is an account that can authenticate and access the API.
type User struct {
	ID           uuid.UUID    `json:"id"`
	Username     string       `json:"username"`
	Email        string       `json:"email"`
	PasswordHash string       `json:"password_hash"`
	Role         UserRoleType `json:"role"`
	IsActive     bool         `json:"is_active"`
	CreatedAt    time.Time    `json:"created_at"`
	UpdatedAt    time.Time    `json:"updated_at"`
}

// RefreshToken stores a hashed refresh token for a user session.
type RefreshToken struct {
	ID        uuid.UUID `json:"id"`
	UserID    uuid.UUID `json:"user_id"`
	TokenHash string    `json:"token_hash"`
	ExpiresAt time.Time `json:"expires_at"`
	Revoked   bool      `json:"revoked"`
	CreatedAt time.Time `json:"created_at"`
}
