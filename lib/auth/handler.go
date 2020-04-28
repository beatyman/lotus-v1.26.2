package auth

import (
	"context"
	"net/http"
	"strings"

	logging "github.com/ipfs/go-log/v2"

	"github.com/filecoin-project/lotus/api"
	"github.com/filecoin-project/lotus/api/apistruct"
)

var log = logging.Logger("auth")

type Handler struct {
	Verify func(ctx context.Context, token string) ([]api.Permission, error)
	Next   http.HandlerFunc
}

func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	ips := []string{r.RemoteAddr}
	ips = append(ips, r.Header["X-Forwarded-For"]...)

	token := r.Header.Get("Authorization")
	if token == "" {
		token = r.FormValue("token")
		if token != "" {
			token = "Bearer " + token
		}
	}

	if token != "" {
		if !strings.HasPrefix(token, "Bearer ") {
			log.Warnf("missing Bearer prefix in auth header %+v", ips)
			w.WriteHeader(401)
			return
		}
		token = strings.TrimPrefix(token, "Bearer ")

		allow, err := h.Verify(ctx, token)
		if err != nil {
			log.Warnf("JWT Verification failed: %s,%+v", err, ips)
			w.WriteHeader(401)
			return
		}

		ctx = apistruct.WithPerm(ctx, allow)
	}

	h.Next(w, r.WithContext(ctx))
}
