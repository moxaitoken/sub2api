//go:build unit

package service

import "testing"

func TestBuildAccountCreationSchedulingDefaultAndExplicitPause(t *testing.T) {
	for _, tc := range []struct {
		name  string
		value *bool
		want  bool
	}{
		{"existing default", nil, true}, {"paused at creation", boolPointerForCreation(false), false}, {"explicit enabled", boolPointerForCreation(true), true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			a, e := buildAccountForCreate(&CreateAccountInput{Name: "fixture", Platform: PlatformAnthropic, Type: AccountTypeOAuth, Credentials: map[string]any{}, Schedulable: tc.value}, map[string]any{})
			if e != nil {
				t.Fatal(e)
			}
			if a.Schedulable != tc.want {
				t.Fatalf("schedulable=%v want %v", a.Schedulable, tc.want)
			}
		})
	}
}
func boolPointerForCreation(v bool) *bool { return &v }
