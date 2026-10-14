# syntax=docker/dockerfile:1
FROM golang:1.26-bookworm AS build
ARG TEMPL_VERSION=v0.3.1070
WORKDIR /src

# templ (the version must match go.mod) turns the .templ files into Go.
RUN go install github.com/a-h/templ/cmd/templ@${TEMPL_VERSION}

COPY go.mod go.sum ./
RUN go mod download
COPY . .

# Not generated here, on purpose:
#  - internal/db (sqlc output) is committed;
#  - web/static/css/app.css (Tailwind output) is committed too. Run `make css` (or `make docker`,
#    which does it for you) before building, so the image never needs to download the Tailwind CLI.
RUN templ generate \
 && CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/patrolhub ./cmd/patrolhub \
 && mkdir -p /out/data/photos

FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /out/patrolhub /patrolhub
# The photo folder must be writable by the non-root user (uid 65532).
COPY --from=build --chown=65532:65532 /out/data /data
ENV HTTP_ADDR=:8080 PHOTO_DIR=/data/photos
USER 65532:65532
EXPOSE 8080
ENTRYPOINT ["/patrolhub", "serve"]
