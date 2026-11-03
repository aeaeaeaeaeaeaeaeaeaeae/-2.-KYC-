FROM golang:1.26-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN go mod tidy && CGO_ENABLED=0 go build -o /out/kyc ./app/cmd

FROM alpine:3.22
COPY --from=build /out/kyc /usr/local/bin/kyc
EXPOSE 8080
ENTRYPOINT ["/usr/local/bin/kyc"]
