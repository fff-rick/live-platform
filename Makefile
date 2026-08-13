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

.PHONY: m7-degradation-smoke m7-connection-sweep m7-hotroom m7-hotroom-ladder m7-slow-consumer m7-like-storm m7-like-ladder m7-gift-load m7-gift-compare m7-soak m7-snapshot m7-fault
m7-degradation-smoke:
	./scripts/m7_degradation_smoke.sh

m7-connection-sweep:
	./scripts/m7_connection_sweep.sh

m7-hotroom:
	./scripts/m7_hotroom.sh

m7-hotroom-ladder:
	./scripts/m7_hotroom_ladder.sh

m7-slow-consumer:
	./scripts/m7_slow_consumer.sh

m7-like-storm:
	./scripts/m7_like_storm.sh

m7-like-ladder:
	./scripts/m7_like_ladder.sh

m7-gift-load:
	./scripts/m7_gift_load.sh

m7-gift-compare:
	./scripts/m7_gift_compare.sh

m7-soak:
	./scripts/m7_soak.sh

m7-snapshot:
	./scripts/m7_snapshot.sh

m7-fault:
	./scripts/m7_fault_injection.sh
