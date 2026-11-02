// Tokio 版订单服务 —— 与 Kaula(kl) / Go 版逐位等价（见 TRANSACTION.md）。
//
// 每连接一个 tokio task：BufReader 读行 → checksum256(256轮, u32回绕)
//   → 手写扫描解析 → Box 分配 64B 业务对象 → 64轮 i32 混合校验(wrapping_*)
//   → sleep 200us（模拟下游 IO）→ 编码 "R,<seq>,<hex8>,<total>\n"
use std::env;
use std::time::Duration;
use tokio::io::{AsyncBufReadExt, AsyncWriteExt, BufReader, BufWriter};
use tokio::net::{TcpListener, TcpStream};

const TOTAL_SLEEP_US: u64 = 200;

/// checksum256：u32 回绕，FNV-1a 变体。与其余两边逐位等价。
fn checksum256(b: &[u8]) -> u32 {
    let mut h: u32 = 0x811C_9DC5;
    for round in 0..256u32 {
        for &c in b {
            h = (h ^ c as u32).wrapping_mul(16777619);
            h = h.wrapping_add(round);
        }
    }
    h
}

/// parse_field：手写扫描第 k 个逗号分隔字段（0 起始），与其余两边同法。
fn parse_field(line: &[u8], k: usize) -> i32 {
    let mut i = 0usize;
    let mut f = 0usize;
    while i < line.len() && f < k {
        if line[i] == b',' {
            f += 1;
        }
        i += 1;
    }
    let mut v: i32 = 0;
    while i < line.len() && line[i] != b',' {
        v = v.wrapping_mul(10).wrapping_add((line[i] - b'0') as i32);
        i += 1;
    }
    v
}

/// 业务对象：堆分配，让分配器进入每请求热路径。
struct BizRecord {
    payload: [u8; 64],
}

async fn handle_conn(stream: TcpStream) {
    let _ = stream.set_nodelay(true);
    let (rd, wr) = stream.into_split();
    let mut r = BufReader::with_capacity(8192, rd);
    let mut w = BufWriter::with_capacity(4096, wr);
    let mut line: Vec<u8> = Vec::with_capacity(128);
    let mut resp: Vec<u8> = Vec::with_capacity(32);
    const HEXDIG: &[u8; 16] = b"0123456789abcdef";

    loop {
        line.clear();
        // read_until 保留分隔符；行体要按不含 \n 的长度算
        match r.read_until(b'\n', &mut line).await {
            Ok(0) | Err(_) => return,
            Ok(_) => {}
        }
        let mut n = line.len();
        while n > 0 && (line[n - 1] == b'\n' || line[n - 1] == b'\r') {
            n -= 1;
        }
        let body = &line[..n];

        let mut ck = checksum256(body);
        let seq = parse_field(body, 1);
        let qty = parse_field(body, 3);
        let price = parse_field(body, 4);
        let total = qty.wrapping_mul(price);

        // S4：堆分配 + 64 轮 i32 混合校验（wrapping ≡ Kaula as<i32> / Go i32）
        let rec = Box::new(BizRecord { payload: [0u8; 64] });
        let mut p = rec.payload;
        p[0] = seq as u8;
        p[1] = qty as u8;
        p[2] = price as u8;
        let mut h: i32 = 0;
        for k in 0..64i32 {
            h = (h ^ p[(k as usize) % 64] as i32)
                .wrapping_add(seq)
                .wrapping_add(qty)
                .wrapping_add(price);
            h = h.wrapping_mul(31).wrapping_add(k);
        }
        ck ^= h as u32;
        drop(rec); // 显式释放，语义与其余两边一致

        // S5：模拟下游 IO
        tokio::time::sleep(Duration::from_micros(TOTAL_SLEEP_US)).await;

        // S6：编码 "R,<seq>,<hex8>,<total>\n"
        resp.clear();
        resp.push(b'R');
        resp.push(b',');
        resp.extend_from_slice(seq.to_string().as_bytes());
        resp.push(b',');
        for s in (0..32).step_by(4).rev() {
            resp.push(HEXDIG[((ck >> s) & 0xF) as usize]);
        }
        resp.push(b',');
        resp.extend_from_slice(total.to_string().as_bytes());
        resp.push(b'\n');

        if w.write_all(&resp).await.is_err() {
            return;
        }
        if w.flush().await.is_err() {
            return;
        }
    }
}

fn main() {
    let port: u16 = env::var("SVC_PORT")
        .ok()
        .and_then(|v| v.parse().ok())
        .unwrap_or(8126);
    let workers: usize = env::var("SVC_WORKERS")
        .ok()
        .and_then(|v| v.parse().ok())
        .unwrap_or(4); // 与 Kaula/Go 边默认一致（TRANSACTION.md §7）

    // 显式 worker_threads：否则测的是机器核数而不是三边同一配置
    let rt = tokio::runtime::Builder::new_multi_thread()
        .worker_threads(workers)
        .enable_all()
        .build()
        .expect("runtime build");

    rt.block_on(async move {
        let addr = format!("0.0.0.0:{}", port);
        let ls = TcpListener::bind(&addr).await.expect("bind");
        eprintln!("[tokio] order server on {} workers={}", addr, workers);
        loop {
            match ls.accept().await {
                Ok((s, _)) => {
                    tokio::spawn(handle_conn(s));
                }
                Err(_) => continue,
            }
        }
    });
}
