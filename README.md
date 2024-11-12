## Booker

### Usage

#### First Run
```shell
booker -c booker.toml -s /Books -o books.json
```

#### Future Runs
```shell
booker -c booker.toml -s /Books -o books.json --retry --cache books.json.old
```

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
