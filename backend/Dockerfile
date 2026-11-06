FROM golang:1.25-alpine AS build
WORKDIR /app
ENV GOTOOLCHAIN=auto
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -o server ./cmd/server

FROM node:22-alpine AS writer
WORKDIR /src
COPY arkiv-writer/package.json arkiv-writer/package-lock.json arkiv-writer/shareSchema.mjs arkiv-writer/write-share.mjs ./
RUN npm ci && chmod +x write-share.mjs

FROM node:22-alpine
RUN apk add --no-cache ca-certificates tzdata
WORKDIR /app
COPY --from=build /app/server .
COPY --from=writer /src /app/arkiv-writer
EXPOSE 8080
CMD ["./server"]
