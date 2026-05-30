use bincode;
use nomt_core::proof::PathProof;
use nomt_core::hasher::Blake3Hasher;
use reqwest::Client;
use serde_json::json;
use std::env;
use bitvec::prelude::*;
use hex::FromHex;

#[tokio::main]
async fn main() -> Result<(), Box<dyn std::error::Error>> {
    let args: Vec<String> = env::args().collect();
    if args.len() < 2 {
        eprintln!("Usage: {} <address> [block_number_or_latest]", args[0]);
        std::process::exit(1);
    }

    let address_str = &args[1];
    let address = address_str.trim_start_matches("0x");
    let _address_bytes = Vec::from_hex(address)?;

    let block_param = if args.len() >= 3 {
        let param = &args[2];
        if param == "latest" || param == "pending" {
            json!(param)
        } else {
            // Hex format block number
            let block_number = if let Ok(num) = param.parse::<u64>() {
                format!("0x{:x}", num)
            } else {
                param.to_string()
            };
            json!(block_number)
        }
    } else {
        json!("latest")
    };

    println!("Fetching proof for address: 0x{} at block: {}", address, block_param);

    // Prepare JSON RPC request
    let client = Client::new();
    let rpc_url = "http://127.0.0.1:8545";
    
    // We pass the raw address bytes as hex (eth_getProof takes 0x... address)
    let req_body = json!({
        "jsonrpc": "2.0",
        "method": "eth_getProof",
        "params": [
            format!("0x{}", address),
            block_param // Block number
        ],
        "id": 1
    });

    let res = client.post(rpc_url)
        .json(&req_body)
        .send()
        .await?;

    let json_res: serde_json::Value = res.json().await?;
    // println!("Full JSON RPC Response: {:?}", json_res);

    let proof_hex = json_res["result"].as_str().ok_or_else(|| format!("Failed to get proof hex from RPC. Response was: {:?}", json_res))?;
    let proof_hex = proof_hex.trim_start_matches("0x");
    let proof_bytes = Vec::from_hex(proof_hex)?;

    println!("Received proof from RPC. Size: {} bytes", proof_bytes.len());

    let proof: PathProof = match bincode::deserialize(&proof_bytes) {
        Ok(p) => p,
        Err(e) => {
            eprintln!("Failed to deserialize proof: {}", e);
            return Ok(());
        }
    };

    println!("Proof deserialized successfully. Siblings count: {}", proof.siblings.len());

    let block_req_body = json!({
        "jsonrpc": "2.0",
        "method": "eth_getBlockByNumber",
        "params": [
            block_param,
            false
        ],
        "id": 2
    });

    println!("Fetching block {} to get real stateRoot...", block_param);
    let block_res = client.post(rpc_url)
        .json(&block_req_body)
        .send()
        .await?;

    let block_json_res: serde_json::Value = block_res.json().await?;
    let state_root_hex = block_json_res["result"]["stateRoot"]
        .as_str()
        .ok_or("Failed to get stateRoot from RPC")?;
    
    let state_root_hex = state_root_hex.trim_start_matches("0x");
    let state_root_bytes = Vec::from_hex(state_root_hex)?;
    
    let mut root = [0u8; 32];
    root.copy_from_slice(&state_root_bytes);

    println!("Real State Root: 0x{}", hex::encode(root));
    
    let bits = proof.terminal.path();

    match proof.verify::<Blake3Hasher>(&bits, root) {
        Ok(verified) => {
            if let Some(leaf) = verified.terminal() {
                println!("Verification SUCCESS! Key exists in trie.");
                println!("Value Hash (Blake3): {:?}", leaf.value_hash);
            } else {
                println!("Verification SUCCESS! Key DOES NOT exist in trie.");
            }
        }
        Err(e) => {
            println!("Verification FAILED: {:?}", e);
        }
    }

    Ok(())
}
