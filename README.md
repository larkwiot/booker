## Booker

### What is Booker
Booker scans your files for ebooks and fetches metadata for them, completely automatically.

### Features
**Providers**
* [Google Books API](https://books.google.com/intl/en/googlebooks/about/index.html)

**Extractors**
* [Apache Tika](https://cwiki.apache.org/confluence/display/TIKA/TikaServer)

### How Does It Work
Inspired by [Ebook Tools](https://github.com/na--/ebook-tools) Booker utilizes extractors and providers to extract
plaintext file contents, scan the contents for identifiers (currently ISBNs), and find metadata based on them.
It then dumps the metadata to a JSON file for you to integrate into whatever system you have.

### Installation

#### With Go / From Source
````shell
go install github.com/larkwiot/booker@latest
````

#### Without Go / Precompiled Release (Linux)
```shell
curl -L -o booker https://github.com/larkwiot/booker/releases/download/v1.0/booker-linux-amd64
wget -O booker https://github.com/larkwiot/booker/releases/download/v1.0/booker-linux-amd64
```

### Quickstart

Stand up a Tika server. You can do this very easily with Docker like so:
```shell
docker run -d --name tika -p9998:9998 apache/tika:latest
```

Once Booker is installed and Tika is up, for your first run, just do this:
```shell
booker -c config.toml.example -s /Books -o books.json
```

And read the Guide while it runs.

For subsequent runs, use this:
```shell
booker -c config.toml.example -s /Books -o books.json.new --cache books.json --retry
```

Again, the below Guide is highly recommended reading.

### Guide

#### Rate Limits & APIs

Providers are rate limited to not get you banned from their APIs. Booker will not impose any limits on your rate limit
configurations, so it is up to you to configure them well.

For Google, the default, authenticated or not request limit is 1,000 per day. If you create a Google Developer account
and add/enable the "Books API" on your account/project, then you can request a quota limit increase. I have no idea
what their approval process looks like internally and have no idea if you will get what you want. I requested an increase
to 30k per day for the development of this project and am waiting on a response.

Providers will disable themselves if they think they have exceeded the rate limit. Currently they detect this by the
HTTP status code 429 "Too Many Requests". If they disable themselves, the only way to get them back up is to stop
Booker and run it again. Booker will continue as long as at least one provider is up, but since (currently) only Google
is implemented, if it disables itself then Booker will be forced to bail out.

#### Threads & Performance

Threads determine how many jobs are run concurrently for extraction and searching combined. You will almost certainly
be bottlenecked by Tika CPU usage and/or provider rate limits before Booker slows down, and since all your threads
will still be subject to the same rate limiter, there will not be a significant advantage to setting this very high,
because the work not inside Booker, it is in Tika and the providers.

For reference, setting the threads to 24 on my 36 thread, 128 GB RAM server nearly consumes 100% of the CPU.

TL;DR I'd recommend keeping your thread count lower, e.g. 32 or less, even on powerful systems.

#### Output, Caching, and Retrying

Booker will completely overwrite the specified output filepath. Because of this, it will complain if the output file
already exists. I realize this could be annoying in cases where you are okay with overwriting, but because APIs have
so many quotas and limits, and because the scanning process is so arduous, I have opted for the safest route whenever
possible to avoid losing work.

Output is written book entry by book entry, so even if Booker closes unexpectedly then you shouldn't lose any work that
was done. That said, the output may be missing an end `}` or something like that, so you might have to manually repair
it to make it fully valid JSON.

By default, if a cache is specified (with `--cache`) retry is not, then Booker will skip ALL entries in the cache
file, even if the entry has an error field. Booker will never modify the cache file.

If retry is specified (with `--retry`), then Booker will only skip the entries from the specified cache if they do NOT
have an error field. Any entry in the cache with an error field will be retried. This is why `--cache` is required
if you want to retry.

As soon as you have any Booker output, it is highly recommended that you use `--cache` to save yourself from redundant
API requests costing you precious API quota tallies.

#### Bug Reporting & Known Issues

Probably **DON'T** report:
* "no results found" in JSON output - This usually (but not always) means the providers disabled themselves to
rate limiting, and you should wait for tomorrow and retry. If you can prove something else went wrong, then go
ahead and make an issue.
* "no texts extracted" in JSON output - This usually means the file wasn't an ebook, or was a really low quality OCR
file for which Tika was unable to extract any meaningful text. If you can manually send the file to Tika and it works,
but it failed in Booker, then run do a --retry run, and if that still doesn't fix it then make an issue.
* "unsolicited HTTP response on idle channel" - You probably got this from Tika, and I get it too sometimes. From all
I've read, it must be a bug in Apache CXF, Tika's HTTP server. Nothing I can do about this, but if you find a workaround
then feel free to report it.

Please **DO** report:
* **ANY** panic - I have specifically written Booker to never panic, so if it ever happens to you, then make an issue
* API changes - If Google or any API moves their endpoints around, changes their default rate limits, or anything like
that, then I want to know.

As with many open source projects, this is a hobby, so issues with PRs or specific code references will get priority.

### Configuration

```toml
[tika]
# change to false to disable Tika
enable = true
# can also specify an IP or wherever you have a Tika server instance
host = "localhost"
# default for Tika, but if you changed the port then specify it here
port = 9998

[google]
# change to false to disable Google
enable = true
# the current Books API endpoint. Can be updated if Google moves it
url = "www.googleapis.com/books/v1/volumes"
# Not required, but default quota is 1,000 req/day.
# Specify your Google Developer API Key here if you have it and
# you can request a quota limit increase with Google
api_key = ""
# defaults to a 1 req/s limit but keep in mind this will use up your (default)
# daily quota in 1000 s or just over 15 minutes. You could set this to 86400
# if you want to ensure that it will never hit your (default) quota but that's
# almost 2 minutes per request, which is going to be really slow if you have a
# decent amount of books and this is your only provider.
milliseconds_per_request = 1000

[advanced]
# defaults to 10k. Keep in mind that increasing this will increase
# the maximum memory usage of Booker, but Tika will still slurp the
# entire file and use tons of memory. I originally intended for this
# setting to contain memory usage but Tika turned out to be the bottleneck.
# The other purpose this setting serves is to ensure that only the ISBN
# for the book listed in the front copyright page is scanned. If you raise
# this too high, then if other ISBNs are mentioned later in the book they
# could get picked up as false positives.
max_characters_to_search_for_isbn = 10000
```

### References & Related Tools / Resources

References
* [Google Books API Documentation](https://developers.google.com/books/docs/overview)
* [Google API Console](https://console.cloud.google.com)
* [Apache Tika API Documentation](https://cwiki.apache.org/confluence/display/TIKA/TikaServer)

Tools
* [jq](https://github.com/jqlang/jq)
* [Calibre](https://github.com/kovidgoyal/calibre)
* [Ebook Tools](https://github.com/na--/ebook-tools)

### Future Work
* Extractor - Python Textract - Not really sure how to integrate Python into a Go tool, but would be really nice to get additional
  extraction logic from Textract.
* Extractor - Tika Embedded - Not sure how possible it is, but embedding Tika could be nice for eliding all the annoyances
of Apache CXF
* Provider - OpenLibrary - At time of writing, Internet Archive is going through some troubles, but once it is back up this
  would be a good provider to add.
* Database - It would be awesome if there were a good way for users of Booker to share their results and merge them
  to save everyone API requests, and the APIs themselves all our redundant traffic.
