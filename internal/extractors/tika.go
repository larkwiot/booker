package extractors

import (
	"context"
	"fmt"
	"github.com/google/go-tika/tika"
	"github.com/larkwiot/booker/internal/book"
	"github.com/larkwiot/booker/internal/config"
	"io"
	"net/http"
	"os"
	"strings"
)

type TikaServer struct {
	url string
	//client *tika.Client
}

func NewTikaServer(conf *config.TikaConfig) *TikaServer {
	return &TikaServer{
		url: fmt.Sprintf("http://%s:%d", conf.Host, conf.Port),
		//client: tika.NewClient(nil, fmt.Sprintf("http://%s:%d", conf.Host, conf.Port)),
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

	// create new client every time because they are not thread safe and I don't
	// want to synchronize just to reuse one when creating them seems to be cheap
	client := tika.NewClient(http.DefaultClient, ts.url)

	body, err := client.ParseReader(context.Background(), fh)
	if err != nil {
		return "", fmt.Errorf("error: tika failed to parse file: %s: %s", bk.Filepath, err.Error())
	}
	defer body.Close()

	text := strings.Builder{}
	count, err := io.CopyN(&text, body, int64(maxCharacters))
	if err != nil {
		return "", fmt.Errorf("error: tika failed to read response buffer into string for file: %s: %s", bk.Filepath, err.Error())
	}

	if uint(count) != maxCharacters {
		return "", fmt.Errorf("error: tika: expected %d bytes to be written but %d were written", maxCharacters, count)
	}

	return text.String(), nil
}
