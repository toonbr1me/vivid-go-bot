package bot

import (
	"context"
	"errors"
	"github.com/mymmrac/telego"
	"io"
	"net/http"
	"time"
)

// Only the server fetches the Telegram URL, which contains the bot token.
func (b *Bot) OpenSupportFile(ctx context.Context, id string) (io.ReadCloser, error) {
	f, err := b.api.GetFile(ctx, &telego.GetFileParams{FileID: id})
	if err != nil {
		return nil, errors.New("attachment lookup failed")
	}
	if f.FilePath == "" {
		return nil, errors.New("attachment unavailable")
	}
	r, err := http.NewRequestWithContext(ctx, http.MethodGet, b.api.FileDownloadURL(f.FilePath), nil)
	if err != nil {
		return nil, errors.New("attachment request failed")
	}
	client := http.Client{Timeout: 30 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	response, err := client.Do(r)
	if err != nil {
		return nil, errors.New("attachment download failed")
	}
	if response.StatusCode != http.StatusOK {
		response.Body.Close()
		return nil, errors.New("attachment download rejected")
	}
	return response.Body, nil
}
