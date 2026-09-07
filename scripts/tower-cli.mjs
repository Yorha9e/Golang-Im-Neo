#!/usr/bin/env node
// Tower stdio bridge: one MCP tools/call to moamcp dist/server.js per invocation.
// usage: node tower-bridge.mjs <tool-name> '<inline-json>' | @payload-file
import { spawn } from 'node:child_process';
import readline from 'node:readline';
import { readFileSync } from 'node:fs';

const SERVER = 'C:/Users/Yorha/.kimi-code/plugins/managed/moamcp/dist/server.js';
const name = process.argv[2];
const payloadArg = process.argv[3];
if (!name || !payloadArg) {
  console.error('usage: node tower-bridge.mjs <tool-name> <inline-json|@file>');
  process.exit(2);
}
const args = JSON.parse(payloadArg.startsWith('@') ? readFileSync(payloadArg.slice(1), 'utf8') : payloadArg);
const child = spawn(process.execPath, [SERVER], { stdio: ['pipe', 'pipe', 'pipe'] });
const rl = readline.createInterface({ input: child.stdout });
let nextId = 1;
let initId = null;
const CALL_ID = 'tower-call-1';
function send(obj) { child.stdin.write(JSON.stringify(obj) + '\n'); }
function call(method, params) { const id = nextId++; send({ jsonrpc: '2.0', id, method, params }); return id; }
rl.on('line', (line) => {
  if (!line.trim()) return;
  let msg; try { msg = JSON.parse(line); } catch { return; }
  if (msg.id === initId) {
    send({ jsonrpc: '2.0', method: 'notifications/initialized' });
    send({ jsonrpc: '2.0', id: CALL_ID, method: 'tools/call', params: { name, arguments: args } });
  } else if (msg.id === CALL_ID) {
    process.stdout.write(JSON.stringify(msg.result ?? msg, null, 1) + '\n');
    child.kill();
    process.exit(0);
  }
});
initId = call('initialize', { protocolVersion: '2024-11-05', capabilities: {}, clientInfo: { name: 'tower-bridge', version: '1.0.0' } });
child.stderr.on('data', (d) => process.stderr.write(d));
child.on('exit', (code) => { if (code && code !== 0) { console.error('server exited', code); process.exit(code); } });
setTimeout(() => { console.error('bridge timeout'); child.kill(); process.exit(3); }, 240000);
