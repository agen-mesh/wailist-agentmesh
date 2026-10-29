package purchase

import (
	"net/http"
	"testing"
)

func TestMerchantEndpointRejectsPrivateTargetsAtPreflightAndDial(t *testing.T) {
	p := NewEndpointPolicy()
	for _, target := range []string{"http://example.com/tool", "https://user:password@example.com/tool", "https://example.com/tool?secret=value", "https://example.com:8443/tool", "https://127.0.0.1/tool", "https://[::1]/tool", "https://169.254.169.254/tool", "https://10.0.0.1/tool"} {
		if err := p.Validate(t.Context(), target); err == nil {
			t.Fatalf("unsafe destination accepted: %s", target)
		}
	}
	request, err := http.NewRequestWithContext(t.Context(), "GET", "https://127.0.0.1/tool", nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = p.Client().Do(request); err == nil {
		t.Fatal("dial admitted private destination")
	}
	if p.Client().CheckRedirect(request, nil) == nil {
		t.Fatal("redirect admitted")
	}
}
