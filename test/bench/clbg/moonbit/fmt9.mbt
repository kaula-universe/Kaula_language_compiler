// 精确的 "%.Nf" 定点格式化：用 bigint 做 round-half-even，与 C printf 对齐。
// 来源：moon-boofuzz@0.1.2 float.mbt (GPL-2.0)，仅提取格式化函数。

///|
fn format_fixed_point(value : Double, precision : Int) -> String {
  let bits = value.reinterpret_as_uint64()
  let zeros = if precision > 0 { "." + "0".repeat(precision) } else { "" }
  if bits << 1 == 0UL {
    return if bits >> 63 == 1UL { "-0" + zeros } else { "0" + zeros }
  }
  if ((bits >> 52) & 0x7ffUL) == 0x7ffUL {
    if bits << 12 != 0UL {
      return if bits >> 63 == 1UL { "-nan" } else { "nan" }
    }
    return if bits >> 63 == 1UL { "-inf" } else { "inf" }
  }
  let negative = bits >> 63 == 1UL
  let magnitude = if negative { -value } else { value }
  // magnitude = m * 2^k with m a 53-bit integer (subnormals keep their
  // raw fraction and the minimum exponent).
  let raw = magnitude.reinterpret_as_uint64()
  let biased = ((raw >> 52) & 0x7ffUL).to_int()
  let (m, k) = if biased == 0 {
    (raw & 0xfffffffffffffUL, -1074)
  } else {
    ((raw & 0xfffffffffffffUL) | 0x10000000000000UL, biased - 1075)
  }
  // digits = round_half_even(m * 10^N * 2^k), computed exactly.
  let scaled = big_from_u64(m)
  for _ in 0..<precision {
    big_mul_small(scaled, 10U)
  }
  if k >= 0 {
    big_shl(scaled, k)
  } else {
    let j = -k
    // Divisor 2^j: r > half iff any bit below j-1 is set; r == half iff
    // bit j-1 is set and no lower bit is. Ties round to even, so the tie
    // bit alone rounds up only when the quotient (bit j of the scaled
    // value) is odd.
    let round_up = big_bit(scaled, j - 1) &&
      (big_any_bit_below(scaled, j - 1) || big_bit(scaled, j))
    big_shr(scaled, j)
    if round_up {
      big_add_one(scaled)
    }
  }
  let digits = big_to_decimal(scaled)
  let body = if precision == 0 {
    digits
  } else {
    let padded = if digits.length() <= precision {
      "0".repeat(precision + 1 - digits.length()) + digits
    } else {
      digits
    }
    let cut = padded.length() - precision
    padded[:cut].to_owned() + "." + padded[cut:].to_owned()
  }
  if negative {
    "-" + body
  } else {
    body
  }
}

///|
fn big_from_u64(v : UInt64) -> Array[UInt] {
  [v.to_uint(), (v >> 32).to_uint()]
}

///|
fn big_mul_small(a : Array[UInt], m : UInt) -> Unit {
  let mut carry : UInt64 = 0UL
  for i in 0..<a.length() {
    let t = a[i].to_uint64() * m.to_uint64() + carry
    a[i] = t.to_uint()
    carry = t >> 32
  }
  if carry != 0UL {
    a.push(carry.to_uint())
  }
}

///|
fn big_bit(a : Array[UInt], i : Int) -> Bool {
  let limb = i / 32
  if limb >= a.length() {
    return false
  }
  ((a[limb] >> (i % 32)) & 1U) == 1U
}

///|
fn big_any_bit_below(a : Array[UInt], i : Int) -> Bool {
  let limb = i / 32
  for j in 0..<limb {
    if j < a.length() && a[j] != 0U {
      return true
    }
  }
  if limb < a.length() {
    let mask = (1U << (i % 32)) - 1U
    if (a[limb] & mask) != 0U {
      return true
    }
  }
  false
}

///|
fn big_shl(a : Array[UInt], bits : Int) -> Unit {
  if bits == 0 {
    return
  }
  let words = bits / 32
  let off = bits % 32
  let n = a.length()
  for _ in 0..<(words + 1) {
    a.push(0U)
  }
  let total = a.length()
  if off == 0 {
    for i = total - 1; i >= 0; i = i - 1 {
      a[i] = if i - words >= 0 && i - words < n { a[i - words] } else { 0U }
    }
    return
  }
  // Written right to left so the sources are never overwritten first.
  for i = total - 1; i >= 0; i = i - 1 {
    let src = i - words
    let mut v : UInt64 = 0UL
    if src >= 1 && src - 1 < n {
      v = a[src - 1].to_uint64() >> (32 - off)
    }
    if src >= 0 && src < n {
      v = v | (a[src].to_uint64() << off)
    }
    a[i] = v.to_uint()
  }
}

///|
fn big_shr(a : Array[UInt], bits : Int) -> Unit {
  let words = bits / 32
  let off = bits % 32
  let n = a.length()
  if words >= n {
    for i in 0..<n {
      a[i] = 0U
    }
    return
  }
  if off == 0 {
    for i in 0..<(n - words) {
      a[i] = a[i + words]
    }
  } else {
    for i in 0..<(n - words) {
      let mut v = (a[i + words].to_uint64() >> off).to_uint()
      if i + words + 1 < n {
        v = v | (a[i + words + 1].to_uint64() << (32 - off)).to_uint()
      }
      a[i] = v
    }
  }
  for i in (n - words)..<n {
    a[i] = 0U
  }
}

///|
fn big_add_one(a : Array[UInt]) -> Unit {
  for i in 0..<a.length() {
    let t = a[i].to_uint64() + 1UL
    a[i] = t.to_uint()
    if t <= 0xffffffffUL {
      return
    }
  }
  a.push(1U)
}

///|
fn big_to_decimal(a : Array[UInt]) -> String {
  let mut n = a.length()
  while n > 1 && a[n - 1] == 0U {
    n -= 1
  }
  if n == 1 && a[0] == 0U {
    return "0"
  }
  let groups : Array[String] = []
  while n > 1 || a[n - 1] != 0U {
    let mut rem : UInt64 = 0UL
    for i = n - 1; i >= 0; i = i - 1 {
      let cur = (rem << 32) | a[i].to_uint64()
      a[i] = (cur / 1000000000UL).to_uint()
      rem = cur % 1000000000UL
    }
    groups.push(rem.to_string())
    while n > 1 && a[n - 1] == 0U {
      n -= 1
    }
  }
  let mut text = groups[groups.length() - 1]
  for i = groups.length() - 2; i >= 0; i = i - 1 {
    text = text + "0".repeat(9 - groups[i].length()) + groups[i]
  }
  text
}

