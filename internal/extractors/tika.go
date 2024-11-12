package extractors

import (
	"fmt"
	"github.com/hashicorp/go-retryablehttp"
	"github.com/larkwiot/booker/internal/book"
	"github.com/larkwiot/booker/internal/config"
	"io"
	"os"
	"strings"
)

type TikaServer struct {
	url string
}

func NewTikaServer(conf *config.TikaConfig) *TikaServer {
	return &TikaServer{
		url: fmt.Sprintf("http://%s:%d/tika", conf.Host, conf.Port),
	}
}

func (ts *TikaServer) Shutdown() {
}

func (ts *TikaServer) Name() string {
	return "Tika"
}

func (ts *TikaServer) ExtractText(bk *book.Book, maxCharacters uint) (string, error) {
	fh, err := os.Open(bk.Filepath)
	if err != nil {
		return "", fmt.Errorf("error: tika unable to open file: %s: %s", bk.Filepath, err.Error())
	}
	defer fh.Close()

	request, err := retryablehttp.NewRequest("PUT", ts.url, fh)
	if err != nil {
		return "", fmt.Errorf("error: unable to create request: %s", err.Error())
	}
	client := retryablehttp.NewClient()
	client.RetryMax = 50
	client.Logger = nil
	response, err := client.Do(request)
	if err != nil {
		return "", fmt.Errorf("error: unable to complete request: %s", err.Error())
	}
	if response.Close {
		defer response.Body.Close()
	}

	text := strings.Builder{}
	count, err := io.CopyN(&text, response.Body, int64(maxCharacters))
	if err != nil {
		return "", fmt.Errorf("error: tika failed to read response buffer into string for file: %s: %s", bk.Filepath, err.Error())
	}

	if uint(count) != maxCharacters {
		return "", fmt.Errorf("error: tika: expected %d bytes to be written but %d were written", maxCharacters, count)
	}

	return text.String(), nil
}
