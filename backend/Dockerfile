# use official Golang image
FROM golang:1.23.1-alpine3.20

# set working directory
WORKDIR /app

# Copy the source code
COPY . .

# Download and install the dependencies
RUN go get -d -v ./...

# Build the Go app
RUN go build -o bin/server cmd/main.go

# Run Database Migration Script
RUN go run cmd/migrate/main.go up

#EXPOSE the port
EXPOSE 9090

# Run the executable
CMD ["./bin/server"]
