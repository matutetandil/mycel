//! A connector Mycel does not ship, written as a plugin.
//!
//! It keeps stock levels in the module's own memory: `write` adjusts a SKU,
//! `read` answers with what is held, and `call` runs a reservation that refuses
//! to take more than there is. Nothing here talks to the outside world, because
//! the point is the boundary — a connector type that is not in the runtime,
//! named by a flow, and answering it.
//!
//! The interface is the one in docs/advanced/wasm.md: alloc/free for memory,
//! and functions taking (ptr, len) and answering with a pointer and a length
//! packed into one i64. Two separate results is the other accepted form, and
//! no toolchain can emit it — a two-word return goes through the C ABI as a
//! pointer argument instead.

use std::alloc::{alloc as raw_alloc, dealloc as raw_dealloc, Layout};
use std::collections::BTreeMap;
use std::sync::Mutex;

use serde::{Deserialize, Serialize};
use serde_json::{json, Value};

// --- Memory ----------------------------------------------------------------

#[no_mangle]
pub extern "C" fn alloc_buffer(size: i32) -> *mut u8 {
    let layout = Layout::from_size_align(size.max(1) as usize, 1).unwrap();
    unsafe { raw_alloc(layout) }
}

#[no_mangle]
pub extern "C" fn free_buffer(ptr: *mut u8, size: i32) {
    if ptr.is_null() {
        return;
    }
    let layout = Layout::from_size_align(size.max(1) as usize, 1).unwrap();
    unsafe { raw_dealloc(ptr, layout) }
}

/// The host looks for these names.
#[no_mangle]
pub extern "C" fn alloc(size: i32) -> *mut u8 {
    alloc_buffer(size)
}

#[no_mangle]
pub extern "C" fn free(ptr: *mut u8, size: i32) {
    free_buffer(ptr, size)
}

/// Hands an answer back: the bytes are leaked into a buffer the host reads and
/// then frees.
fn answer(value: Value) -> i64 {
    let bytes = serde_json::to_vec(&value).unwrap_or_else(|_| b"{}".to_vec());
    let len = bytes.len();
    let ptr = alloc_buffer(len as i32);
    unsafe { std::ptr::copy_nonoverlapping(bytes.as_ptr(), ptr, len) };
    ((ptr as i64) << 32) | (len as i64)
}

fn input(ptr: *const u8, len: i32) -> Value {
    if ptr.is_null() || len <= 0 {
        return Value::Null;
    }
    let slice = unsafe { std::slice::from_raw_parts(ptr, len as usize) };
    serde_json::from_slice(slice).unwrap_or(Value::Null)
}

// --- What the connector holds ----------------------------------------------

#[derive(Default, Serialize, Deserialize)]
struct Stock {
    levels: BTreeMap<String, i64>,
    warehouse: String,
}

static STOCK: Mutex<Option<Stock>> = Mutex::new(None);

fn with_stock<T>(f: impl FnOnce(&mut Stock) -> T) -> T {
    let mut guard = STOCK.lock().unwrap();
    let stock = guard.get_or_insert_with(Stock::default);
    f(stock)
}

// --- The interface ----------------------------------------------------------

/// init receives the connector's configuration, which is whatever the flow's
/// connector block declared.
#[no_mangle]
pub extern "C" fn init(ptr: *const u8, len: i32) -> i64 {
    let config = input(ptr, len);

    let warehouse = config
        .get("warehouse")
        .and_then(Value::as_str)
        .unwrap_or("default")
        .to_string();

    if warehouse.is_empty() {
        return answer(json!({ "error": "warehouse is required" }));
    }

    with_stock(|stock| {
        stock.warehouse = warehouse.clone();
        // Opening stock, so a read before any write has something to answer.
        stock.levels.insert("WIDGET-1".into(), 10);
        stock.levels.insert("WIDGET-2".into(), 3);
    });

    answer(json!({ "ok": true }))
}

#[no_mangle]
pub extern "C" fn health(_ptr: *const u8, _len: i32) -> i64 {
    answer(json!({ "ok": true }))
}

#[no_mangle]
pub extern "C" fn close(_ptr: *const u8, _len: i32) -> i64 {
    with_stock(|stock| stock.levels.clear());
    answer(json!({ "ok": true }))
}

/// read answers with the stock held, optionally for one SKU.
#[no_mangle]
pub extern "C" fn read(ptr: *const u8, len: i32) -> i64 {
    let query = input(ptr, len);
    let wanted = query
        .get("filters")
        .and_then(|f| f.get("sku"))
        .and_then(Value::as_str)
        .map(str::to_string);

    with_stock(|stock| {
        let rows: Vec<Value> = stock
            .levels
            .iter()
            .filter(|(sku, _)| wanted.as_deref().map_or(true, |w| w == sku.as_str()))
            .map(|(sku, level)| {
                json!({ "sku": sku, "on_hand": level, "warehouse": stock.warehouse })
            })
            .collect();

        answer(json!({ "rows": rows }))
    })
}

/// write adjusts a SKU by the amount given, and answers with what it became.
#[no_mangle]
pub extern "C" fn write(ptr: *const u8, len: i32) -> i64 {
    let data = input(ptr, len);
    let payload = data.get("payload").cloned().unwrap_or(Value::Null);

    let sku = match payload.get("sku").and_then(Value::as_str) {
        Some(sku) if !sku.is_empty() => sku.to_string(),
        _ => return answer(json!({ "error": "sku is required" })),
    };
    let delta = payload
        .get("delta")
        .and_then(Value::as_i64)
        .or_else(|| payload.get("delta").and_then(Value::as_str).and_then(|s| s.parse().ok()))
        .unwrap_or(0);

    with_stock(|stock| {
        let level = stock.levels.entry(sku.clone()).or_insert(0);
        *level += delta;
        answer(json!({
            "affected": 1,
            "rows": [{ "sku": sku, "on_hand": *level }],
        }))
    })
}

/// call runs an operation that is neither a read nor a write: reserving stock,
/// which is refused when there is not enough of it.
#[no_mangle]
pub extern "C" fn call(ptr: *const u8, len: i32) -> i64 {
    let request = input(ptr, len);
    let operation = request.get("operation").and_then(Value::as_str).unwrap_or("");
    let params = request.get("params").cloned().unwrap_or(Value::Null);

    match operation {
        "reserve" => {
            let sku = params.get("sku").and_then(Value::as_str).unwrap_or("").to_string();
            let quantity = params.get("quantity").and_then(Value::as_i64).unwrap_or(0);

            with_stock(|stock| match stock.levels.get_mut(&sku) {
                None => answer(json!({ "error": format!("no stock record for {sku}") })),
                Some(level) if *level < quantity => answer(json!({
                    "error": format!("only {level} of {sku} on hand, {quantity} asked for")
                })),
                Some(level) => {
                    *level -= quantity;
                    answer(json!({
                        "data": { "sku": sku, "reserved": quantity, "remaining": *level }
                    }))
                }
            })
        }
        other => answer(json!({ "error": format!("this connector has no operation {other}") })),
    }
}
