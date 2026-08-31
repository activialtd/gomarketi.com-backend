// jwt-helper is a one-off cross-language interop tool, not production code.
// It exists purely so services/admin-api/scripts/smoke-jwt.ts can prove that
// tokens signed by the Go auth service's shared/pkg/jwt.Manager (RS256) are
// verifiable by Node's jsonwebtoken, and vice versa, before any admin-api
// route depends on that assumption.
package main

import (
	"encoding/json"
	"fmt"
	"os"
	"time"

	pkgjwt "github.com/activialtd/gomarketi.com-backend/shared/pkg/jwt"
)

func main() {
	if len(os.Args) < 2 {
		fail("usage: jwt-helper mint|verify [token]")
	}

	priv, err := pkgjwt.LoadPrivateKey("../../../../keys/private.pem")
	if err != nil {
		fail("load private key: %v", err)
	}
	pub, err := pkgjwt.LoadPublicKey("../../../../keys/public.pem")
	if err != nil {
		fail("load public key: %v", err)
	}
	mgr, err := pkgjwt.NewManager(pkgjwt.Config{PrivateKey: priv, PublicKey: pub, AccessTokenTTL: 5 * time.Minute})
	if err != nil {
		fail("new manager: %v", err)
	}

	switch os.Args[1] {
	case "mint":
		// Optional 2nd arg overrides the subject — default kept as
		// "smoke-test-user-id" so scripts/smoke-jwt.ts's assertions
		// (which expect exactly that value) keep passing unchanged.
		subject := "smoke-test-user-id"
		if len(os.Args) >= 3 {
			subject = os.Args[2]
		}
		token, err := mgr.IssueAccessToken(subject, pkgjwt.Claims{IsBuyer: true})
		if err != nil {
			fail("issue token: %v", err)
		}
		fmt.Print(token)
	case "verify":
		if len(os.Args) < 3 {
			fail("usage: jwt-helper verify <token>")
		}
		claims, err := mgr.ValidateClaims(os.Args[2])
		if err != nil {
			fail("VERIFY_FAILED: %v", err)
		}
		out, _ := json.Marshal(claims)
		fmt.Print(string(out))
	default:
		fail("unknown command %q", os.Args[1])
	}
}

func fail(format string, a ...any) {
	fmt.Fprintf(os.Stderr, format+"\n", a...)
	os.Exit(1)
}
