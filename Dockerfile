FROM docker.io/library/node:24-alpine AS web
WORKDIR /web
COPY web/package.json web/package-lock.json ./
RUN npm ci
COPY web ./
RUN npm run build

FROM docker.io/library/golang:1.26-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY cmd ./cmd
COPY internal ./internal
COPY api ./api
COPY web/*.go ./web/
COPY --from=web /web/dist ./web/dist
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/oh-my-story ./cmd/oh-my-story

FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /out/oh-my-story /oh-my-story
USER nonroot:nonroot
EXPOSE 8080
ENTRYPOINT ["/oh-my-story"]
CMD ["serve"]
