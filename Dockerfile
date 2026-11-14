FROM docker.io/library/golang:1.26-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY cmd ./cmd
COPY internal ./internal
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/oh-my-story ./cmd/oh-my-story

FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /out/oh-my-story /oh-my-story
USER nonroot:nonroot
EXPOSE 8080
ENTRYPOINT ["/oh-my-story"]
CMD ["serve"]
