FROM golang:1.26 AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN go generate ./pkg/db/... ./pkg/ui/... ./uikit/... && CGO_ENABLED=0 go build -ldflags "-s -w" -o /zotrem ./cmd/zotrem

FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /zotrem /zotrem
ENV ZOTREM_DATA_DIR=/data
VOLUME /data
EXPOSE 8000
ENTRYPOINT ["/zotrem"]
CMD ["serve", "--bind", "0.0.0.0", "--port", "8000"]
