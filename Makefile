.PHONY: run worker test vet fmt compose-up compose-down compose-reset smoke-m2 smoke-m3 smoke-m4 smoke-m5 smoke-m6 test-m4-concurrency test-m5-kafka-recovery

run:
	go run ./cmd/api

worker:
	go run ./cmd/worker

test:
	go test ./...

vet:
	go vet ./...

fmt:
	gofmt -w $$(find . -name '*.go')

compose-up:
	docker compose up --build

compose-down:
	docker compose down

compose-reset:
	docker compose down -v
	docker compose up --build

smoke-m2:
	./scripts/m2_smoke.sh

smoke-m3:
	./scripts/m3_smoke.sh

smoke-m4:
	./scripts/m4_smoke.sh

test-m4-concurrency:
	./scripts/m4_concurrency.sh

smoke-m5:
	./scripts/m5_smoke.sh

test-m5-kafka-recovery:
	./scripts/m5_kafka_recovery.sh

smoke-m6:
	./scripts/m6_observability_smoke.sh
