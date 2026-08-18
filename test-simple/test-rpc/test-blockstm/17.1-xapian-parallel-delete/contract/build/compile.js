const fs = require('fs');
const solc = require('solc');
const path = require('path');

const solFile = process.argv[2];
const buildDir = process.argv[3];
const sourceCode = fs.readFileSync(solFile, 'utf8');

const input = {
    language: 'Solidity',
    sources: { [path.basename(solFile)]: { content: sourceCode } },
    settings: {
        evmVersion: 'london', // FIX OPCODES ERRORS (NO PUSH0)
        outputSelection: { '*': { '*': ['abi', 'evm.bytecode.object'] } }
    }
};

const output = JSON.parse(solc.compile(JSON.stringify(input)));

if (output.errors) {
    let hasError = false;
    output.errors.forEach(err => {
        console.error(err.formattedMessage);
        if (err.severity === 'error') hasError = true;
    });
    if (hasError) process.exit(1);
}

for (let file in output.contracts) {
    for (let contractName in output.contracts[file]) {
        const contract = output.contracts[file][contractName];
        const abiPath = path.join(buildDir, file.replace('.sol', '') + "_" + contractName + ".abi");
        const binPath = path.join(buildDir, file.replace('.sol', '') + "_" + contractName + ".bin");
        
        fs.writeFileSync(abiPath, JSON.stringify(contract.abi, null, 2));
        fs.writeFileSync(binPath, contract.evm.bytecode.object);
    }
}
