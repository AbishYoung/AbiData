package forms

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"strconv"
	"strings"
	"time"

	"abidata/internal/fault"
	"abidata/internal/workspace"
)

// Confirm continuations are opaque, server-issued tokens: the handler name the
// form's ui.confirm chose, an expiry and an HMAC binding both to the workspace,
// form and user. A client can therefore only resume a continuation the form's
// own code offered to that user, never call an arbitrary module function.
// The key is per process: a restart only means "please try again".
const confirmTTL = 15 * time.Minute

var confirmKey = func() []byte {
	k := make([]byte, 32)
	if _, err := rand.Read(k); err != nil {
		panic(err)
	}
	return k
}()

func confirmMAC(ws, formID, userID, name string, exp int64) string {
	m := hmac.New(sha256.New, confirmKey)
	for _, s := range []string{ws, formID, userID, name, strconv.FormatInt(exp, 10)} {
		m.Write([]byte(s))
		m.Write([]byte{0})
	}
	return base64.RawURLEncoding.EncodeToString(m.Sum(nil))
}

func sealContinuation(l *workspace.Lease, c Caller, formID, name string, now time.Time) string {
	exp := now.Add(confirmTTL).Unix()
	return name + "." + strconv.FormatInt(exp, 10) + "." + confirmMAC(l.ID().String(), formID, c.UserID, name, exp)
}

// openContinuation verifies a token and returns the handler name.
func openContinuation(l *workspace.Lease, c Caller, formID, token string, now time.Time) (string, error) {
	expired := fault.New(fault.BadRequest, "This confirmation is no longer valid. Repeat the action and confirm again.")
	parts := strings.Split(token, ".")
	if len(parts) != 3 || !identRe.MatchString(parts[0]) || FormEvents[parts[0]] || ControlEvents[parts[0]] {
		return "", expired
	}
	exp, err := strconv.ParseInt(parts[1], 10, 64)
	if err != nil || now.Unix() > exp {
		return "", expired
	}
	want := confirmMAC(l.ID().String(), formID, c.UserID, parts[0], exp)
	if !hmac.Equal([]byte(want), []byte(parts[2])) {
		return "", expired
	}
	return parts[0], nil
}

// fold folds the session into resp and seals any confirm continuation.
func (s *Service) fold(l *workspace.Lease, c Caller, p *prepared, resp *EventResponse) {
	p.fold(resp)
	if resp.Confirm != nil && !strings.Contains(resp.Confirm.Continuation, ".") {
		cf := *resp.Confirm
		cf.Continuation = sealContinuation(l, c, p.form.ID, cf.Continuation, time.Now())
		resp.Confirm = &cf
	}
}
