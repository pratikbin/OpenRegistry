package types

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/duo-labs/webauthn/webauthn"

	"github.com/go-playground/validator/v10"
)

type (
	User struct {
		CreatedAt         time.Time `json:"created_at,omitempty" validate:"-"`
		UpdatedAt         time.Time `json:"updated_at,omitempty" validate:"-"`
		TwitterUsername   string    `json:"twitter_username,omitempty"`
		Password          string    `json:"password,omitempty"`
		Username          string    `json:"username,omitempty" validate:"-"`
		Email             string    `json:"email,omitempty" validate:"email"`
		URL               string    `json:"url,omitempty"`
		Company           string    `json:"company,omitempty"`
		ReceivedEventsURL string    `json:"received_events_url,omitempty"`
		Bio               string    `json:"bio,omitempty"`
		Type              string    `json:"type,omitempty"`
		GravatarID        string    `json:"gravatar_id,omitempty"`
		Id                string    `json:"uuid,omitempty" validate:"-"`
		HTMLURL           string    `json:"html_url,omitempty"`
		Location          string    `json:"location,omitempty"`
		Login             string    `json:"login,omitempty"`
		Name              string    `json:"name,omitempty"`
		NodeID            string    `json:"node_id,omitempty"`
		OrganizationsURL  string    `json:"organizations_url,omitempty"`
		AvatarURL         string    `json:"avatar_url,omitempty"`
		credentials       []webauthn.Credential
		OAuthID           int  `json:"id,omitempty"`
		IsActive          bool `json:"is_active,omitempty" validate:"-"`
		Hireable          bool `json:"hireable,omitempty"`
	}

	OAuthUser struct {
		UpdatedAt         time.Time `json:"updated_at"`
		CreatedAt         time.Time `json:"created_at"`
		Location          string    `json:"location"`
		ReceivedEventsURL string    `json:"received_events_url"`
		Email             string    `json:"email"`
		Bio               string    `json:"bio"`
		Type              string    `json:"type"`
		GravatarID        string    `json:"gravatar_id"`
		TwitterUsername   string    `json:"twitter_username"`
		HTMLURL           string    `json:"html_url"`
		Company           string    `json:"company"`
		Login             string    `json:"login"`
		Name              string    `json:"name"`
		NodeID            string    `json:"node_id"`
		OrganizationsURL  string    `json:"organizations_url"`
		AvatarURL         string    `json:"avatar_url"`
		URL               string    `json:"url"`
		FKID              string
		ID                int  `json:"id"`
		Hireable          bool `json:"hireable"`
	}
	Session struct {
		Id           string `json:"id"`
		RefreshToken string `json:"refresh_token"`
		Owner        string `json:"-"`
	}
)

func (u *User) Validate() error {
	if u == nil {
		return fmt.Errorf("user is nil")
	}

	v := validator.New()

	return v.Struct(u)
}

func (u *User) Bytes() ([]byte, error) {
	if u == nil {
		return nil, fmt.Errorf("user struct is nil")
	}

	return json.Marshal(u)
}

// WebAuthnID - User ID according to the Relying Party
func (u *User) WebAuthnID() []byte {
	return []byte(u.Id)
}

// WebAuthnName - User Name according to the Relying Party
func (u *User) WebAuthnName() string {
	return u.Username
}

// WebAuthnDisplayName - Display Name of the user
func (u *User) WebAuthnDisplayName() string {
	return u.Username
}

// WebAuthnIcon - User's icon url
func (u *User) WebAuthnIcon() string {
	return u.AvatarURL
}

// WebAuthnCredentials - Credentials owned by the user
func (u *User) WebAuthnCredentials() []webauthn.Credential {
	return u.credentials
}

func (u *User) AddWebAuthNCredential(creds *webauthn.Credential) {
	u.credentials = append(u.credentials, *creds)
}
