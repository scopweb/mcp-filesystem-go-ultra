package core

import "testing"

func TestIsSecretPath(t *testing.T) {
	yes := []string{".env", ".env.local", "id_rsa", "id_rsa.pub", "foo.pem", "x.key", "a.p12", "credentials.json"}
	no := []string{"main.go", "readme.md", "env.txt", "keyring.go"}
	for _, p := range yes {
		if !IsSecretPath(p) {
			t.Errorf("expected secret: %s", p)
		}
	}
	for _, p := range no {
		if IsSecretPath(p) {
			t.Errorf("not secret: %s", p)
		}
	}
}
