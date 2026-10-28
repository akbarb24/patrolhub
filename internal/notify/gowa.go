// Package notify sends messages to the RT's WhatsApp group through a GOWA gateway
// (github.com/aldinokemal/go-whatsapp-web-multidevice, see deploy/GOWA.md).
package notify

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/akbarbinaji/patrolhub/internal/config"
)

// timeout bounds one send so a hung gateway cannot stall the closer loop.
const timeout = 15 * time.Second

// GOWA posts text messages to one WhatsApp group.
type GOWA struct {
	cfg    config.WhatsApp
	client *http.Client
}

func NewGOWA(cfg config.WhatsApp) *GOWA {
	return &GOWA{cfg: cfg, client: &http.Client{Timeout: timeout}}
}

// SendGroup sends text to the configured group. GOWA answers errors with a non-2xx status.
func (g *GOWA) SendGroup(ctx context.Context, text string) error {
	body, err := json.Marshal(map[string]string{"phone": g.cfg.GroupJID, "message": text})
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimRight(g.cfg.URL, "/")+"/send/message", bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	if g.cfg.User != "" {
		req.SetBasicAuth(g.cfg.User, g.cfg.Password)
	}
	res, err := g.client.Do(req)
	if err != nil {
		return fmt.Errorf("gowa send: %w", err)
	}
	defer res.Body.Close()
	if res.StatusCode/100 != 2 {
		msg, _ := io.ReadAll(io.LimitReader(res.Body, 200))
		return fmt.Errorf("gowa send: %s: %s", res.Status, strings.TrimSpace(string(msg)))
	}
	io.Copy(io.Discard, res.Body)
	return nil
}
