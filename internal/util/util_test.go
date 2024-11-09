package util_test

import (
	"github.com/larkwiot/booker/internal/book"
	"github.com/larkwiot/booker/internal/util"
	"github.com/stretchr/testify/assert"
	"regexp"
	"testing"
)

const howToHackLikeAGhost = "            <p>ISBN-13: 978-1-7185-0126-3 (print) \nISBN-13: 978-1-7185-0127-0 (ebook)\n</p>\nIdentifiers: LCCN 2020052503 (print) | LCCN 2020052504 (ebook) | ISBN \n   9781718501263 (paperback) | ISBN 1718501269 (paperback) | ISBN \n   9781718501270 (ebook)  \nSubjects: LCSH: Computer networks--Security measures. | Hacking. | Cloud \n   computing--Security measures. | Penetration testing (Computer networks) \nClassification: LCC TK5105.59 .F624 2021  (print) | LCC TK5105.59  (ebook) \n   | DDC 005.8/7--dc23 \nLC record available at https://lccn.loc.gov/2020052503\nLC ebook record available at https://lccn.loc.gov/2020052504\n</p>"

func TestRegexes(t *testing.T) {
	matches := regexp.MustCompile(util.Isbn10Pattern).FindAllString(howToHackLikeAGhost, -1)
	assert.Equal(t, 18, len(matches))

	matches = regexp.MustCompile(util.Isbn13Pattern).FindAllString(howToHackLikeAGhost, -1)
	assert.Equal(t, 18, len(matches))
}

func TestIdentifyIsbn10s(t *testing.T) {
	isbns := util.IdentifyIsbn10s(howToHackLikeAGhost)
	assert.Equal(t, []book.ISBN10{"2020052504", "1718501269", "2020052504"}, isbns)
}

func TestIdentifyIsbn13s(t *testing.T) {

}
