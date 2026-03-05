.PHONY: build dev clean css sqlc

build: css
	go build -o bin/flock ./cmd/flock

dev:
	go run ./cmd/flock

clean:
	rm -rf bin/

css:
	tailwindcss -i web/input.css -o web/static/styles.css --minify -c web/tailwind.config.js

sqlc:
	sqlc generate
