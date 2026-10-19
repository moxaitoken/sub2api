package supplier

import infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"

var (
	ErrNotSupplier          = infraerrors.Forbidden("SUPPLIER_REQUIRED", "supplier access required")
	ErrSupplierDisabled     = infraerrors.Forbidden("SUPPLIER_DISABLED", "supplier is disabled")
	ErrSupplierNotFound     = infraerrors.NotFound("SUPPLIER_NOT_FOUND", "supplier not found")
	ErrSupplierExists       = infraerrors.Conflict("SUPPLIER_EXISTS", "user is already a supplier")
	ErrUserNotFound         = infraerrors.BadRequest("SUPPLIER_USER_NOT_FOUND", "user not found")
	ErrAccountNotFound      = infraerrors.NotFound("SUPPLIER_ACCOUNT_NOT_FOUND", "account not found")
	ErrAccountAlreadyLinked = infraerrors.Conflict("SUPPLIER_ACCOUNT_LINKED", "account already belongs to a supplier")
	ErrAccountLimitReached  = infraerrors.Forbidden("SUPPLIER_ACCOUNT_LIMIT", "supplier account limit reached")
	ErrProxyNotFound        = infraerrors.NotFound("SUPPLIER_PROXY_NOT_FOUND", "proxy not found")
	ErrProxyAlreadyLinked   = infraerrors.Conflict("SUPPLIER_PROXY_LINKED", "proxy already belongs to a supplier")
	ErrOAuthSessionNotFound = infraerrors.BadRequest("SUPPLIER_OAUTH_SESSION_NOT_FOUND", "authorization session not found or expired")
	ErrInvalidAuthMode      = infraerrors.BadRequest("SUPPLIER_INVALID_AUTH_MODE", "mode must be oauth or setup-token")
	ErrInvalidPeriod        = infraerrors.BadRequest("SUPPLIER_INVALID_PERIOD", "invalid time range")
	ErrInvalidReview        = infraerrors.BadRequest("SUPPLIER_INVALID_REVIEW", "action must be approve or reject")
	ErrSettlementNotFound   = infraerrors.NotFound("SUPPLIER_SETTLEMENT_NOT_FOUND", "settlement not found")
	ErrSettlementOverlap    = infraerrors.Conflict("SUPPLIER_SETTLEMENT_OVERLAP", "period overlaps an existing settlement")
	ErrSettlementStatus     = infraerrors.Conflict("SUPPLIER_SETTLEMENT_STATUS", "settlement status does not allow this action")
)
