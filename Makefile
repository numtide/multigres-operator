.PHONY: check-coverage
check-coverage:
	go test ./... -coverprofile=./cover.out -covermode=atomic -coverpkg=./...
	go tool cover -html=cover.out -o=cover.html
	echo now open cover.html