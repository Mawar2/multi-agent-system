// Package load contains load and stress tests for Kaimi's core packages.
//
// These tests verify correctness and absence of race conditions under high
// concurrency. Run with -race to detect data races:
//
//	go test -race -v ./test/load/...
//
// Four scenarios are defined:
//
//	Scenario 1: Store - 100 concurrent writers and 50 concurrent readers on JSONStore
//	Scenario 2: Agent - 50 workers draining 100+ opportunities through StubAgent
//	Scenario 3: SAM.gov client - 100 concurrent cached FetchByNAICS calls
//	Scenario 4: Full pipeline - 100+ opportunities through fetch → store → agent with 50 workers
//
// Benchmarks support CPU and memory profiling with pprof:
//
//	go test -bench=. -cpuprofile=cpu.out -memprofile=mem.out ./test/load/...
//	go tool pprof cpu.out
package load
