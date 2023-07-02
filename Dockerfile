# syntax=docker/dockerfile:1

FROM golang:1.20

WORKDIR /app

COPY go.mod go.sum ./
RUN go mod download

COPY . ./

RUN CGO_ENABLED=0 GOOS=linux go build -tags wss -o /poc ./cmd/poc

EXPOSE 8000

CMD ["/poc"]


FROM tinygo:0.28.1

WORKDIR /tinygo

COPY go.mod go.sum ./
RUN go mod download

COPY . ./

RUN tinygo version
RUN tinygo build -target pico -o /main.uf2 ./cmd/tinygo
