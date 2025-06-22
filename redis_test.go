package redisFallback

import (
	"context"
	"fmt"
	"log"
	"sync"
	"time"
)

func main() {
	fmt.Println("=== Redis Fallback Package Test Suite ===")

	// Configuration setup
	config := Config{
		Redis: &Redis{
			Host:     "localhost",
			Port:     6379,
			Password: "0123456789",
			DB:       0,
		},
		Log: &Log{
			Stdout: true,
		},
		// 不設定 Options，使用預設值
	}

	// Create Redis Fallback instance
	cache, err := New(config)
	if err != nil {
		log.Fatalf("Failed to create cache instance: %v", err)
	}
	defer cache.Close()

	fmt.Println("✓ Redis Fallback instance created successfully")

	// Test suite execution
	testResults := make(map[string]bool)

	testResults["Basic Operations"] = testBasicOperations(cache)
	testResults["TTL Operations"] = testTTLOperations(cache)
	testResults["Complex Data Types"] = testComplexDataTypes(cache)
	testResults["Batch Operations"] = testBatchOperations(cache)
	testResults["Concurrent Operations"] = testConcurrentOperations(cache)
	testResults["Delete Operations"] = testDeleteOperations(cache)
	testResults["Error Handling"] = testErrorHandling(cache)

	// Interactive fallback mode test
	fmt.Println("\n--- Manual Fallback Mode Test ---")
	fmt.Println("Run this test manually by stopping/starting Redis during execution:")
	fmt.Println("Uncomment the line below to test fallback mode")
	// testFallbackMode(cache)

	// Print test summary
	printTestSummary(testResults)

	fmt.Println("\nKeeping program alive for 30 seconds to observe logs...")
	time.Sleep(30 * time.Second)
}

func testBasicOperations(cache *RedisFallback) bool {
	fmt.Println("\n--- Testing Basic Operations ---")
	success := true

	// Test string operations
	if err := cache.Set("test:string", "Hello Redis Fallback", 10*time.Minute); err != nil {
		fmt.Printf("✗ Failed to set string: %v\n", err)
		success = false
	} else {
		fmt.Println("✓ String set successfully")
	}

	if value, err := cache.Get("test:string"); err == nil {
		fmt.Printf("✓ String retrieved: %v\n", value)
	} else {
		fmt.Printf("✗ Failed to get string: %v\n", err)
		success = false
	}

	// Test numeric operations
	if err := cache.Set("test:integer", 42, 5*time.Minute); err != nil {
		fmt.Printf("✗ Failed to set integer: %v\n", err)
		success = false
	} else {
		fmt.Println("✓ Integer set successfully")
	}

	if value, err := cache.Get("test:integer"); err == nil {
		fmt.Printf("✓ Integer retrieved: %v\n", value)
	} else {
		fmt.Printf("✗ Failed to get integer: %v\n", err)
		success = false
	}

	// Test float operations
	if err := cache.Set("test:float", 3.14159, 5*time.Minute); err != nil {
		fmt.Printf("✗ Failed to set float: %v\n", err)
		success = false
	} else {
		fmt.Println("✓ Float set successfully")
	}

	// Test boolean operations
	if err := cache.Set("test:boolean", true, 5*time.Minute); err != nil {
		fmt.Printf("✗ Failed to set boolean: %v\n", err)
		success = false
	} else {
		fmt.Println("✓ Boolean set successfully")
	}

	return success
}

func testTTLOperations(cache *RedisFallback) bool {
	fmt.Println("\n--- Testing TTL Operations ---")
	success := true

	// Test short TTL
	if err := cache.Set("test:ttl:short", "expires soon", 2*time.Second); err != nil {
		fmt.Printf("✗ Failed to set short TTL: %v\n", err)
		success = false
	} else {
		fmt.Println("✓ Short TTL data set successfully")
	}

	// Verify data exists immediately
	if value, err := cache.Get("test:ttl:short"); err == nil {
		fmt.Printf("✓ Short TTL data retrieved immediately: %v\n", value)
	} else {
		fmt.Printf("✗ Failed to get short TTL data immediately: %v\n", err)
		success = false
	}

	// Wait for expiration
	fmt.Println("Waiting 3 seconds for data to expire...")
	time.Sleep(3 * time.Second)

	// Verify data is expired
	if _, err := cache.Get("test:ttl:short"); err != nil && err.Error() == "Not found" {
		fmt.Println("✓ Short TTL data correctly expired")
	} else {
		fmt.Println("✗ Short TTL data did not expire correctly")
		success = false
	}

	// Test permanent data (no TTL)
	if err := cache.Set("test:ttl:permanent", "never expires", 0); err != nil {
		fmt.Printf("✗ Failed to set permanent data: %v\n", err)
		success = false
	} else {
		fmt.Println("✓ Permanent data set successfully")
	}

	// Test long TTL
	if err := cache.Set("test:ttl:long", "expires later", 1*time.Hour); err != nil {
		fmt.Printf("✗ Failed to set long TTL: %v\n", err)
		success = false
	} else {
		fmt.Println("✓ Long TTL data set successfully")
	}

	return success
}

func testComplexDataTypes(cache *RedisFallback) bool {
	fmt.Println("\n--- Testing Complex Data Types ---")
	success := true

	// Test map/struct
	userData := map[string]interface{}{
		"id":       1001,
		"username": "johndoe",
		"email":    "john.doe@example.com",
		"age":      28,
		"active":   true,
		"tags":     []string{"developer", "golang", "redis"},
		"metadata": map[string]interface{}{
			"last_login":  time.Now().Unix(),
			"login_count": 42,
			"preferences": []string{"dark_mode", "notifications"},
		},
	}

	if err := cache.Set("test:user:complex", userData, 15*time.Minute); err != nil {
		fmt.Printf("✗ Failed to set complex user data: %v\n", err)
		success = false
	} else {
		fmt.Println("✓ Complex user data set successfully")
	}

	if value, err := cache.Get("test:user:complex"); err == nil {
		fmt.Printf("✓ Complex user data retrieved: %+v\n", value)
	} else {
		fmt.Printf("✗ Failed to get complex user data: %v\n", err)
		success = false
	}

	// Test array of objects
	products := []map[string]interface{}{
		{"id": 1, "name": "Laptop", "price": 999.99, "in_stock": true},
		{"id": 2, "name": "Mouse", "price": 29.99, "in_stock": false},
		{"id": 3, "name": "Keyboard", "price": 79.99, "in_stock": true},
	}

	if err := cache.Set("test:products:list", products, 20*time.Minute); err != nil {
		fmt.Printf("✗ Failed to set product list: %v\n", err)
		success = false
	} else {
		fmt.Println("✓ Product list set successfully")
	}

	// Test nested structures
	config := map[string]interface{}{
		"app": map[string]interface{}{
			"name":    "MyApp",
			"version": "1.0.0",
			"features": map[string]bool{
				"analytics": true,
				"debugging": false,
				"caching":   true,
			},
		},
		"database": map[string]interface{}{
			"host": "localhost",
			"port": 5432,
			"connections": map[string]int{
				"max_idle": 10,
				"max_open": 100,
			},
		},
	}

	if err := cache.Set("test:config:nested", config, 30*time.Minute); err != nil {
		fmt.Printf("✗ Failed to set nested config: %v\n", err)
		success = false
	} else {
		fmt.Println("✓ Nested config set successfully")
	}

	return success
}

func testBatchOperations(cache *RedisFallback) bool {
	fmt.Println("\n--- Testing Batch Operations ---")
	success := true
	start := time.Now()

	// Batch set operations
	batchSize := 200
	for i := 0; i < batchSize; i++ {
		key := fmt.Sprintf("test:batch:item:%d", i)
		value := map[string]interface{}{
			"id":        i,
			"name":      fmt.Sprintf("Item %d", i),
			"timestamp": time.Now().Unix(),
			"batch_id":  "batch_001",
		}

		if err := cache.Set(key, value, 30*time.Minute); err != nil {
			fmt.Printf("✗ Failed to set batch item %d: %v\n", i, err)
			success = false
		}
	}

	elapsed := time.Since(start)
	fmt.Printf("✓ Batch set %d items completed in %v\n", batchSize, elapsed)

	// Verify random samples
	testIndices := []int{10, 50, 100, 150, 199}
	for _, idx := range testIndices {
		key := fmt.Sprintf("test:batch:item:%d", idx)
		if value, err := cache.Get(key); err == nil {
			fmt.Printf("✓ Verified batch item %d: %v\n", idx, value)
		} else {
			fmt.Printf("✗ Failed to verify batch item %d: %v\n", idx, err)
			success = false
		}
	}

	return success
}

func testConcurrentOperations(cache *RedisFallback) bool {
	fmt.Println("\n--- Testing Concurrent Operations ---")
	success := true
	start := time.Now()

	numWorkers := 100
	var wg sync.WaitGroup
	var mu sync.Mutex
	errors := 0

	for i := 0; i < numWorkers; i++ {
		wg.Add(1)
		go func(workerID int) {
			defer wg.Done()

			key := fmt.Sprintf("test:concurrent:worker:%d", workerID)
			value := map[string]interface{}{
				"worker_id": workerID,
				"timestamp": time.Now().UnixNano(),
				"data":      fmt.Sprintf("Concurrent data from worker %d", workerID),
			}

			// Set operation
			if err := cache.Set(key, value, 25*time.Minute); err != nil {
				mu.Lock()
				errors++
				mu.Unlock()
				return
			}

			// Immediate read verification
			if _, err := cache.Get(key); err != nil {
				mu.Lock()
				errors++
				mu.Unlock()
			}
		}(i)
	}

	wg.Wait()
	elapsed := time.Since(start)

	if errors == 0 {
		fmt.Printf("✓ Concurrent operations with %d workers completed successfully in %v\n", numWorkers, elapsed)
	} else {
		fmt.Printf("✗ Concurrent operations completed with %d errors in %v\n", errors, elapsed)
		success = false
	}

	return success
}

func testDeleteOperations(cache *RedisFallback) bool {
	fmt.Println("\n--- Testing Delete Operations ---")
	success := true

	// Set up test data
	testKeys := []string{
		"test:delete:string",
		"test:delete:number",
		"test:delete:object",
	}

	for i, key := range testKeys {
		value := fmt.Sprintf("Delete test data %d", i)
		if err := cache.Set(key, value, 10*time.Minute); err != nil {
			fmt.Printf("✗ Failed to set test data for deletion: %v\n", err)
			success = false
			continue
		}
	}

	// Verify data exists before deletion
	for _, key := range testKeys {
		if _, err := cache.Get(key); err != nil {
			fmt.Printf("✗ Test data not found before deletion: %s\n", key)
			success = false
		}
	}

	// Perform deletions
	for _, key := range testKeys {
		if err := cache.Del(key); err != nil {
			fmt.Printf("✗ Failed to delete %s: %v\n", key, err)
			success = false
		} else {
			fmt.Printf("✓ Successfully deleted %s\n", key)
		}
	}

	// Verify data is deleted
	for _, key := range testKeys {
		if _, err := cache.Get(key); err == nil {
			fmt.Printf("✗ Data still exists after deletion: %s\n", key)
			success = false
		} else {
			fmt.Printf("✓ Confirmed deletion of %s\n", key)
		}
	}

	return success
}

func testErrorHandling(cache *RedisFallback) bool {
	fmt.Println("\n--- Testing Error Handling ---")
	success := true

	// Test getting non-existent key
	if _, err := cache.Get("test:nonexistent:key"); err != nil {
		if err.Error() == "Not found" {
			fmt.Println("✓ Correctly handled non-existent key")
		} else {
			fmt.Printf("✗ Unexpected error for non-existent key: %v\n", err)
			success = false
		}
	} else {
		fmt.Println("✗ Should have returned error for non-existent key")
		success = false
	}

	// Test deleting non-existent key (should not error)
	if err := cache.Del("test:nonexistent:delete"); err != nil {
		fmt.Printf("✗ Unexpected error deleting non-existent key: %v\n", err)
		success = false
	} else {
		fmt.Println("✓ Gracefully handled deletion of non-existent key")
	}

	return success
}

func testFallbackMode(cache *RedisFallback) {
	fmt.Println("\n--- Testing Fallback Mode ---")
	fmt.Println("This test writes data continuously. Stop/restart Redis during execution to test fallback.")
	fmt.Println("Test duration: 60 seconds")

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	ticker := time.NewTicker(1 * time.Second)
	defer ticker.Stop()

	counter := 0
	for {
		select {
		case <-ctx.Done():
			fmt.Println("Fallback mode test completed")
			return
		case <-ticker.C:
			counter++
			key := fmt.Sprintf("test:fallback:entry:%d", counter)
			value := map[string]interface{}{
				"counter":   counter,
				"timestamp": time.Now().Format("2006-01-02 15:04:05"),
				"message":   fmt.Sprintf("Fallback test entry #%d", counter),
			}

			// Write operation
			writeErr := cache.Set(key, value, 30*time.Minute)
			writeStatus := "✓"
			if writeErr != nil {
				writeStatus = "✗"
			}

			// Read operation
			readValue, readErr := cache.Get(key)
			readStatus := "✓"
			if readErr != nil {
				readStatus = "✗"
			}

			fmt.Printf("[%s] Write: %s %s | Read: %s %v\n",
				time.Now().Format("15:04:05"), writeStatus, key, readStatus, readValue != nil)
		}
	}
}

func printTestSummary(results map[string]bool) {
	fmt.Println("\n=== Test Summary ===")
	passed := 0
	total := len(results)

	for testName, result := range results {
		status := "✗ FAILED"
		if result {
			status = "✓ PASSED"
			passed++
		}
		fmt.Printf("%s %s\n", status, testName)
	}

	fmt.Printf("\nResults: %d/%d tests passed (%.1f%%)\n",
		passed, total, float64(passed)/float64(total)*100)

	if passed == total {
		fmt.Println("🎉 All tests passed!")
	} else {
		fmt.Printf("❌ %d test(s) failed\n", total-passed)
	}
}
