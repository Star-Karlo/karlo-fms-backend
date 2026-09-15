# Multi-stage build for any FMS application service under ./cmd.
#   docker build --build-arg SERVICE=api -t fms-api .
FROM golang:1.25-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
ARG SERVICE
RUN test -n "$SERVICE" || (echo "SERVICE build-arg required" && exit 1)
RUN CGO_ENABLED=0 go build -trimpath -o /out/app ./cmd/${SERVICE}
# The schema migrator rides in every image alongside the migrations it
# applies, so a deploy can run it as a one-off task from the SAME image it is
# about to roll — no second build, no checkout, and the migrations in the
# task are exactly the ones the binary was built with. Needed where the
# database has no public address (Jakarta) and CI cannot reach it with psql.
RUN CGO_ENABLED=0 go build -trimpath -o /out/migrate-schema ./cmd/migrate-schema

FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /out/app /app
COPY --from=build /out/migrate-schema /migrate-schema
COPY --from=build /src/db/migrations /migrations
ENV MIGRATIONS_DIR=/migrations
ENTRYPOINT ["/app"]
