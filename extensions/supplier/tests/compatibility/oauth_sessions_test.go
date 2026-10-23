// These tests execute the unchanged upstream oauth.go helper in a temporary
// standard-library-only package. They do not call Claude or validate the gateway.
package oauth

import (
	"net/url"
	"testing"
	"time"
)

func TestSupplierOAuthSessionExpires(t *testing.T) {
	store := NewSessionStore()
	defer store.Stop()
	store.Set("expired-fixture", &OAuthSession{CreatedAt: time.Now().Add(-SessionTTL - time.Second)})
	if _, ok := store.Get("expired-fixture"); ok {
		t.Fatal("expired session must not be returned")
	}
	store.Set("fresh-fixture", &OAuthSession{CreatedAt: time.Now()})
	if _, ok := store.Get("fresh-fixture"); !ok {
		t.Fatal("fresh session should be available")
	}
}

func TestSupplierOAuthSessionDelete(t *testing.T) {
	store := NewSessionStore()
	defer store.Stop()
	store.Set("fixture", &OAuthSession{CreatedAt: time.Now()})
	store.Delete("fixture")
	if _, ok := store.Get("fixture"); ok {
		t.Fatal("deleted session must not be returned")
	}
}

func TestSupplierOAuthSessionReadDoesNotConsume(t *testing.T) {
	store := NewSessionStore()
	defer store.Stop()
	store.Set("fixture", &OAuthSession{CreatedAt: time.Now()})
	for i := 0; i < 2; i++ {
		if _, ok := store.Get("fixture"); !ok {
			t.Fatal("Get unexpectedly consumed the session")
		}
	}
	// A Get-then-Delete flow is not an atomic single-consumer primitive. The
	// supplier service must serialize exchange requests for the same flow.
}

func TestSupplierOAuthNewStoreHasNoPreviousSession(t *testing.T) {
	first := NewSessionStore()
	defer first.Stop()
	first.Set("fixture", &OAuthSession{CreatedAt: time.Now()})
	second := NewSessionStore()
	defer second.Stop()
	if _, ok := second.Get("fixture"); ok {
		t.Fatal("independent memory stores must not share sessions")
	}
}

func TestSupplierOAuthAuthorizationURLPreservesFlowFields(t *testing.T) {
	u, err := url.Parse(BuildAuthorizationURL("fixture-state", "fixture-challenge", ScopeOAuth))
	if err != nil {
		t.Fatal(err)
	}
	for key, expected := range map[string]string{
		"state": "fixture-state", "code_challenge": "fixture-challenge",
		"code_challenge_method": "S256", "redirect_uri": RedirectURI,
		"scope": ScopeOAuth, "client_id": ClientID,
	} {
		if u.Query().Get(key) != expected {
			t.Fatalf("%s did not preserve the expected value", key)
		}
	}
}
