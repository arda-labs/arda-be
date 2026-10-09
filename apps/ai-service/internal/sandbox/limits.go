package sandbox

import (
	"errors"
	"runtime/metrics"
	"sync"
	"time"

	"github.com/dlclark/regexp2/v2"
	"github.com/dop251/goja"
)

const (
	// MaxHeapGrowthBytes bounds how far the process heap may grow while one
	// script runs. Goja has no per-VM allocator limit, so a watchdog compares
	// the process heap with the value sampled at start. The bound is generous
	// on purpose: it must only fire for runaway scripts, never for the garbage
	// concurrent requests produce.
	MaxHeapGrowthBytes = 256 << 20
	// MaxCallStackSize bounds JS recursion depth.
	MaxCallStackSize = 512
	// maxNativeLength caps the size of strings and arrays handed to builtins
	// that loop or allocate natively. A native call cannot be interrupted, so
	// the size must be refused before it starts.
	maxNativeLength = 1 << 20
	// regexMatchTimeout bounds backtracking regexes (lookahead/backreference
	// patterns run on the backtracking engine, which Interrupt cannot stop).
	regexMatchTimeout = time.Second

	heapWatchInterval = 25 * time.Millisecond
)

var ErrSandboxMemory = errors.New("ai.sandbox_memory_exceeded: script allocated too much memory")

func init() {
	// Process-wide, but the sandbox is the only regexp2 user in this service.
	regexp2.DefaultMatchTimeout = regexMatchTimeout
}

// nativeLimitShim wraps builtins whose cost scales with a length argument or
// receiver length. It runs before the script and before the SDK is injected.
const nativeLimitShim = `(function () {
	var LIMIT = ` + "1048576" + `;
	function tooLarge() { throw new RangeError("ai.sandbox_value_too_large"); }
	function wrap(proto, name, check) {
		var original = proto[name];
		if (typeof original !== "function") { return; }
		Object.defineProperty(proto, name, {
			value: function () {
				if (check(this, arguments)) { tooLarge(); }
				return original.apply(this, arguments);
			},
			writable: true, configurable: true, enumerable: false
		});
	}
	var arrayMethods = ["fill", "join", "map", "filter", "forEach", "reduce", "reduceRight",
		"some", "every", "indexOf", "lastIndexOf", "includes", "slice", "splice", "concat",
		"sort", "reverse", "flat", "flatMap", "find", "findIndex", "findLast", "findLastIndex",
		"keys", "values", "entries", "copyWithin", "at", "push", "unshift"];
	arrayMethods.forEach(function (name) {
		wrap(Array.prototype, name, function (self) { return self != null && self.length > LIMIT; });
	});
	wrap(String.prototype, "repeat", function (self, args) { return String(self).length * Number(args[0]) > LIMIT; });
	["padStart", "padEnd"].forEach(function (name) {
		wrap(String.prototype, name, function (self, args) { return Number(args[0]) > LIMIT; });
	});
})();`

// hardenFunctionConstructors removes the constructor property from every
// function prototype. Without it, (function(){})["con"+"structor"] reaches
// Function and evaluates arbitrary code, bypassing both the deleted eval and
// the static validator, which only sees literal names.
const hardenFunctionConstructors = `(function () {
	var samples = [function () {}, async function () {}, function* () {}];
	samples.forEach(function (fn) {
		var proto = Object.getPrototypeOf(fn);
		Object.defineProperty(proto, "constructor", { value: undefined, writable: false, configurable: false });
	});
})();`

func installNativeLimits(vm *goja.Runtime) error {
	if _, err := vm.RunString(nativeLimitShim); err != nil {
		return err
	}
	_, err := vm.RunString(hardenFunctionConstructors)
	return err
}

func heapObjectsBytes() uint64 {
	sample := []metrics.Sample{{Name: "/memory/classes/heap/objects:bytes"}}
	metrics.Read(sample)
	if sample[0].Value.Kind() != metrics.KindUint64 {
		return 0
	}
	return sample[0].Value.Uint64()
}

// startHeapWatchdog interrupts the VM when the process heap grows past
// MaxHeapGrowthBytes. The returned function stops the watchdog and waits for
// it, so the goroutine never outlives the execution.
func startHeapWatchdog(vm *goja.Runtime) func() {
	baseline := heapObjectsBytes()
	done := make(chan struct{})
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		ticker := time.NewTicker(heapWatchInterval)
		defer ticker.Stop()
		for {
			select {
			case <-done:
				return
			case <-ticker.C:
				if current := heapObjectsBytes(); current > baseline && current-baseline > MaxHeapGrowthBytes {
					vm.Interrupt(ErrSandboxMemory.Error())
					return
				}
			}
		}
	}()
	return func() {
		close(done)
		wg.Wait()
	}
}
