FROM golang:1.21-alpine

WORKDIR /app

# Install dependencies for CGO (SQLite needs it)
RUN apk add --no-cache gcc musl-dev sqlite

# Copy all files
COPY . .

# Create storage directories
RUN mkdir -p storage/uploads

# Download Go modules
RUN go mod tidy

# Expose port
EXPOSE 8080

# Run the application
CMD ["go", "run", "cmd/main.go"]