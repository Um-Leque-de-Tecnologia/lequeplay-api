# Build de um binário numa imagem distroless.
# Selecione o alvo com --build-arg BIN=api|seed (default: api).
FROM golang:1.26 AS build
ARG BIN=api
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/app ./cmd/${BIN}

FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /out/app /app
USER nonroot:nonroot
ENTRYPOINT ["/app"]
