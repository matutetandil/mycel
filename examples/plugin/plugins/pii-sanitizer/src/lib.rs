//! A sanitiser Mycel does not ship, written as a plugin.
//!
//! The built-in pipeline refuses the attacks everybody has — SQL fragments,
//! XML entities, null bytes, control characters. What it cannot know is what
//! counts as sensitive in your business: this one masks card numbers and
//! refuses anything carrying a New Zealand IRD number, because those must not
//! reach a log, a queue or a third party.
//!
//! It receives the value a flow was handed, as JSON, and answers with the
//! value to use instead. Answering with nothing rejects the input, and the
//! request is refused before any flow runs.

use std::alloc::{alloc as raw_alloc, dealloc as raw_dealloc, Layout};

use serde_json::Value;

#[no_mangle]
pub extern "C" fn alloc(size: i32) -> *mut u8 {
    let layout = Layout::from_size_align(size.max(1) as usize, 1).unwrap();
    unsafe { raw_alloc(layout) }
}

#[no_mangle]
pub extern "C" fn free(ptr: *mut u8, size: i32) {
    if ptr.is_null() {
        return;
    }
    let layout = Layout::from_size_align(size.max(1) as usize, 1).unwrap();
    unsafe { raw_dealloc(ptr, layout) }
}

/// Hands an answer back: a pointer and a length packed into one i64. A length
/// of zero is a rejection.
fn answer(value: Value) -> i64 {
    let bytes = match serde_json::to_vec(&value) {
        Ok(bytes) => bytes,
        Err(_) => return 0,
    };
    let len = bytes.len();
    let ptr = alloc(len as i32);
    unsafe { std::ptr::copy_nonoverlapping(bytes.as_ptr(), ptr, len) };
    ((ptr as i64) << 32) | (len as i64)
}

/// looks_like_an_ird recognises a New Zealand tax number: eight or nine digits,
/// usually written with dashes.
fn looks_like_an_ird(text: &str) -> bool {
    let digits: String = text.chars().filter(char::is_ascii_digit).collect();
    (digits.len() == 8 || digits.len() == 9)
        && text.chars().all(|c| c.is_ascii_digit() || c == '-' || c == ' ')
        && text.contains('-')
}

/// mask_card replaces all but the last four digits of anything long enough to
/// be a card number.
fn mask_card(text: &str) -> Option<String> {
    let digits: String = text.chars().filter(char::is_ascii_digit).collect();
    if digits.len() < 13 || digits.len() > 19 {
        return None;
    }
    if !text.chars().all(|c| c.is_ascii_digit() || c == ' ' || c == '-') {
        return None;
    }
    Some(format!("****{}", &digits[digits.len() - 4..]))
}

/// clean walks a value, masking what it can and reporting what it cannot let
/// through.
fn clean(value: &Value) -> Result<Value, ()> {
    match value {
        Value::String(text) => {
            if looks_like_an_ird(text) {
                return Err(());
            }
            if let Some(masked) = mask_card(text) {
                return Ok(Value::String(masked));
            }
            Ok(value.clone())
        }
        Value::Array(items) => {
            let mut cleaned = Vec::with_capacity(items.len());
            for item in items {
                cleaned.push(clean(item)?);
            }
            Ok(Value::Array(cleaned))
        }
        Value::Object(fields) => {
            let mut cleaned = serde_json::Map::with_capacity(fields.len());
            for (name, item) in fields {
                cleaned.insert(name.clone(), clean(item)?);
            }
            Ok(Value::Object(cleaned))
        }
        _ => Ok(value.clone()),
    }
}

/// sanitize is the entry point the rule calls.
#[no_mangle]
pub extern "C" fn sanitize(ptr: *const u8, len: i32) -> i64 {
    if ptr.is_null() || len <= 0 {
        return 0;
    }
    let slice = unsafe { std::slice::from_raw_parts(ptr, len as usize) };

    let value: Value = match serde_json::from_slice(slice) {
        Ok(value) => value,
        // Not JSON at all: nothing here can vouch for it.
        Err(_) => return 0,
    };

    match clean(&value) {
        Ok(cleaned) => answer(cleaned),
        // Rejected: the request is refused before any flow runs.
        Err(()) => 0,
    }
}
