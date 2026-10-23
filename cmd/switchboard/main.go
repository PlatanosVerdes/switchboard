// switchboard is the only reader of a Telegram bot shared by several services: it takes each update
// and hands it to the service whose prefix it carries. The services answer Telegram themselves.
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"reflect"
	"strconv"
	"syscall"
	"time"

	"github.com/PlatanosVerdes/switchboard/internal/route"
)

var buildVersion = "local"

const (
	apiURL      = "https://api.telegram.org/bot"
	pollSeconds = 50
	menuEvery   = 5 * time.Minute
)

func main() {
	log := slog.New(slog.NewTextHandler(os.Stderr, nil))
	if os.Getenv("SWITCHBOARD_LOG_JSON") == "1" {
		log = slog.New(slog.NewJSONHandler(os.Stderr, nil))
	}
	if err := run(log); err != nil {
		log.Error("switchboard stopped", "err", err)
		os.Exit(1)
	}
}

func run(log *slog.Logger) error {
	token := os.Getenv("SWITCHBOARD_TELEGRAM_TOKEN")
	if token == "" {
		return errors.New("SWITCHBOARD_TELEGRAM_TOKEN is not set")
	}
	table, err := route.Parse(os.Getenv("SWITCHBOARD_ROUTES"), os.Getenv("SWITCHBOARD_FALLBACK"))
	if err != nil {
		return fmt.Errorf("SWITCHBOARD_ROUTES: %w", err)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	sb := &switchboard{
		token:  token,
		table:  table,
		log:    log,
		client: &http.Client{Timeout: (pollSeconds + 10) * time.Second},
	}

	port := os.Getenv("SWITCHBOARD_PORT")
	if port == "" {
		port = "8000"
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprintf(w, `{"status":"ok","version":%q}`, buildVersion)
	})
	srv := &http.Server{Addr: ":" + port, Handler: mux, ReadHeaderTimeout: 10 * time.Second}
	go func() {
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Error("http server stopped", "err", err)
			stop()
		}
	}()

	go sb.keepMenu(ctx)
	log.Info("listening", "addr", srv.Addr, "routes", len(table.Routes), "version", buildVersion)
	sb.poll(ctx)

	shutdown, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	return srv.Shutdown(shutdown)
}

type switchboard struct {
	token  string
	table  route.Table
	log    *slog.Logger
	client *http.Client
}

func (s *switchboard) poll(ctx context.Context) {
	// -1 answers the last update alone, so a restart skips the backlog.
	offset := int64(-1)
	for ctx.Err() == nil {
		updates, err := s.updates(ctx, offset)
		if err != nil {
			if ctx.Err() != nil {
				return
			}
			s.log.Error("could not read the updates", "err", err)
			select {
			case <-ctx.Done():
				return
			case <-time.After(10 * time.Second):
			}
			continue
		}
		for _, raw := range updates {
			var head struct {
				UpdateID int64 `json:"update_id"`
			}
			_ = json.Unmarshal(raw, &head)
			offset = head.UpdateID + 1
			target, ok := s.table.For(raw)
			if !ok {
				s.log.Info("update with no service dropped", "update", head.UpdateID)
				continue
			}
			// A slow service must not hold the others' updates.
			go s.forward(ctx, target, raw)
		}
	}
}

func (s *switchboard) updates(ctx context.Context, offset int64) ([]json.RawMessage, error) {
	form := url.Values{
		"timeout":         {strconv.Itoa(pollSeconds)},
		"offset":          {strconv.FormatInt(offset, 10)},
		"allowed_updates": {`["message","callback_query"]`},
	}
	var answer struct {
		OK          bool              `json:"ok"`
		Description string            `json:"description"`
		Result      []json.RawMessage `json:"result"`
	}
	if err := s.telegram(ctx, "getUpdates", form, &answer); err != nil {
		return nil, err
	}
	if !answer.OK {
		return nil, fmt.Errorf("getUpdates: %s", answer.Description)
	}
	return answer.Result, nil
}

func (s *switchboard) forward(ctx context.Context, target string, raw json.RawMessage) {
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, target+"/telegram/update", bytes.NewReader(raw))
	if err != nil {
		s.log.Error("could not build the forward", "target", target, "err", err)
		return
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := s.client.Do(req)
	if err != nil {
		s.log.Error("could not forward the update", "target", target, "err", err)
		return
	}
	resp.Body.Close()
	if resp.StatusCode >= 300 {
		s.log.Error("the service refused the update", "target", target, "status", resp.StatusCode)
	}
}

type command struct {
	Name        string `json:"command"`
	Description string `json:"description"`
}

// keepMenu publishes every service's commands as the bot's menu, and again whenever one changes.
func (s *switchboard) keepMenu(ctx context.Context) {
	var published []command
	for {
		menu := s.menu(ctx)
		if len(menu) > 0 && !reflect.DeepEqual(menu, published) {
			encoded, _ := json.Marshal(menu)
			var answer struct {
				OK          bool   `json:"ok"`
				Description string `json:"description"`
			}
			err := s.telegram(ctx, "setMyCommands", url.Values{"commands": {string(encoded)}}, &answer)
			switch {
			case err != nil:
				s.log.Warn("could not publish the menu", "err", err)
			case !answer.OK:
				s.log.Warn("could not publish the menu", "err", answer.Description)
			default:
				published = menu
				s.log.Info("menu published", "commands", len(menu))
			}
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(menuEvery):
		}
	}
}

func (s *switchboard) menu(ctx context.Context) []command {
	var menu []command
	for _, r := range s.table.Routes {
		ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
		req, _ := http.NewRequestWithContext(ctx, http.MethodGet, r.URL+"/telegram/commands", nil)
		resp, err := s.client.Do(req)
		if err != nil {
			cancel()
			s.log.Warn("could not read a service's commands", "target", r.URL, "err", err)
			continue
		}
		var cmds []command
		err = json.NewDecoder(resp.Body).Decode(&cmds)
		resp.Body.Close()
		cancel()
		if err != nil {
			s.log.Warn("a service's commands are not readable", "target", r.URL, "err", err)
			continue
		}
		menu = append(menu, cmds...)
	}
	return menu
}

func (s *switchboard) telegram(ctx context.Context, method string, form url.Values, out any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, apiURL+s.token+"/"+method,
		bytes.NewBufferString(form.Encode()))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := s.client.Do(req)
	if err != nil {
		// The URL holds the token; keep it out of the logs.
		var uerr *url.Error
		if errors.As(err, &uerr) {
			return uerr.Err
		}
		return err
	}
	defer resp.Body.Close()
	return json.NewDecoder(resp.Body).Decode(out)
}
