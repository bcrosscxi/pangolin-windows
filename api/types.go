//go:build windows

package api

// APIResponse is the wrapper structure for all API responses
type APIResponse[T any] struct {
	Success *bool  `json:"success,omitempty"`
	Error   *bool  `json:"error,omitempty"`
	Status  int    `json:"status,omitempty"`
	Message string `json:"message,omitempty"`
	Data    T      `json:"data,omitempty"`
}

// EmptyResponse represents an empty API response
type EmptyResponse struct{}

// LoginRequest represents a login request
type LoginRequest struct {
	Email    string  `json:"email"`
	Password string  `json:"password"`
	Code     *string `json:"code,omitempty"`
}

// LoginResponse represents a login response
type LoginResponse struct {
	UserId                    string  `json:"userId"`
	Email                     string  `json:"email"`
	Username                  *string `json:"username,omitempty"`
	Name                      *string `json:"name,omitempty"`
	CodeRequested             *bool   `json:"codeRequested,omitempty"`
	EmailVerificationRequired *bool   `json:"emailVerificationRequired,omitempty"`
}

// DeviceAuthStartRequest represents a device auth start request
type DeviceAuthStartRequest struct {
	ApplicationName string  `json:"applicationName"`
	DeviceName      *string `json:"deviceName,omitempty"`
}

// DeviceAuthStartResponse represents a device auth start response
type DeviceAuthStartResponse struct {
	Code         string `json:"code"`
	URL          string `json:"url"`
	ExpiresIn    int    `json:"expiresIn"`
	ExpiresAt    int64  `json:"expiresAt"`
	PollInterval int    `json:"pollInterval"`
}

// DeviceAuthPollResponse represents a device auth poll response
type DeviceAuthPollResponse struct {
	Verified bool    `json:"verified"`
	Token    *string `json:"token,omitempty"`
	Message  *string `json:"message,omitempty"`
}

// User represents a user
type User struct {
	Id       string  `json:"id"`
	UserId   string  `json:"userId"` // Alias for Id, used in some contexts
	Email    string  `json:"email"`
	Username *string `json:"username,omitempty"`
	Name     *string `json:"name,omitempty"`
}

// ListUserOrgsResponse represents the response for listing user organizations
type ListUserOrgsResponse struct {
	Orgs []Org `json:"orgs"`
}

// Org represents an organization
type Org struct {
	Id   string `json:"id"`
	Name string `json:"name"`
}

// CreateOlmRequest represents a request to create an OLM
type CreateOlmRequest struct {
	Name string `json:"name"`
}

// CreateOlmResponse represents a response from creating an OLM
type CreateOlmResponse struct {
	Id     string `json:"id"`
	OlmId  string `json:"olmId"`
	Secret string `json:"secret"`
	Name   string `json:"name"`
}

// GetOrgResponse represents the response for getting an organization
type GetOrgResponse struct {
	Id   string `json:"id"`
	Name string `json:"name"`
}

// CheckOrgUserAccessResponse represents the response for checking org user access
type CheckOrgUserAccessResponse struct {
	Allowed  bool         `json:"allowed"`
	Error    *string      `json:"error,omitempty"`
	Policies *OrgPolicies `json:"policies,omitempty"`
}

// OrgPolicies represents organization policies
type OrgPolicies struct {
	RequiredTwoFactor *bool             `json:"requiredTwoFactor,omitempty"`
	MaxSessionLength  *MaxSessionLength `json:"maxSessionLength,omitempty"`
	PasswordAge       *PasswordAge      `json:"passwordAge,omitempty"`
}

// MaxSessionLength represents max session length policy
type MaxSessionLength struct {
	Compliant             bool `json:"compliant"`
	MaxSessionLengthHours int  `json:"maxSessionLengthHours"`
	SessionAgeHours       int  `json:"sessionAgeHours"`
}

// PasswordAge represents password age policy
type PasswordAge struct {
	Compliant          bool `json:"compliant"`
	MaxPasswordAgeDays int  `json:"maxPasswordAgeDays"`
	PasswordAgeDays    int  `json:"passwordAgeDays"`
}

// GetClientResponse represents the response for getting a client
type GetClientResponse struct {
	Id    int     `json:"id"`
	Name  string  `json:"name"`
	OlmId *string `json:"olmId,omitempty"`
}
