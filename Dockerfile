FROM golang:1.21-alpine

WORKDIR /app

COPY . .

RUN go mod tidy
RUN go build -o app ./backend

EXPOSE 8080

CMD ["./app"]