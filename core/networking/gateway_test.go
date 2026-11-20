package networking

import (
	"context"
	"crypto/tls"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"
	"time"
)

func TestGatewayRejectsUnlistedHosts(t *testing.T) {
	for _, sni := range []string{"", "example.com"} {
		req := httptest.NewRequest("GET", "/", nil)
		if sni != "" {
			req.Header.Set("X-SNI", sni)
		}
		rec := httptest.NewRecorder()
		newHandler().ServeHTTP(rec, req)
		if rec.Header().Get(ErrorHeader) == "" {
			t.Errorf("sni %q: gateway error not marked with %s", sni, ErrorHeader)
		}
		if rec.Code != http.StatusForbidden && rec.Code != http.StatusBadRequest {
			t.Errorf("sni %q: status %d", sni, rec.Code)
		}
	}
}

func TestHandshakeOffersOnlyHTTP1(t *testing.T) {
	protocols := make(chan []string, 1)
	server := httptest.NewUnstartedServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	server.TLS = &tls.Config{
		GetConfigForClient: func(hello *tls.ClientHelloInfo) (*tls.Config, error) {
			protocols <- hello.SupportedProtos
			return nil, nil
		},
	}
	server.StartTLS()
	defer server.Close()

	d := &echDialer{}
	conn, _ := d.handshake(context.Background(), server.Listener.Addr().String(), "localhost", nil)
	if conn != nil {
		conn.Close()
	}
	select {
	case got := <-protocols:
		if !reflect.DeepEqual(got, []string{"http/1.1"}) {
			t.Fatalf("offered protocols = %v", got)
		}
	case <-time.After(time.Second):
		t.Fatal("server did not receive a TLS ClientHello")
	}
}

func TestStartIsIdempotent(t *testing.T) {
	defer Stop()
	if err := Start("127.0.0.1:0"); err != nil {
		t.Fatal(err)
	}
	if err := Start("127.0.0.1:0"); err != nil {
		t.Fatal(err)
	}
}
