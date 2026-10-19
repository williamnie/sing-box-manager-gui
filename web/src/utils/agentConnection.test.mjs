import test from 'node:test';
import assert from 'node:assert/strict';
import { execFileSync } from 'node:child_process';
import { agentConnectCommand } from './agentConnection.ts';

test('uses the actual management port and automatically includes TLS pin', () => {
  const command = agentConnectCommand({ server: 'https://192.168.50.142:19090', fingerprint: 'a'.repeat(64), allow_insecure_http: false });
  assert.ok(command.includes("--server 'https://192.168.50.142:19090'"));
  assert.ok(command.includes(`--fingerprint '${'a'.repeat(64)}'`));
  assert.ok(!command.includes('--allow-insecure-http'));
});

test('HTTP connection is explicit and does not invent a certificate pin', () => {
  assert.equal(agentConnectCommand({ server: 'http://192.168.50.142:19090', fingerprint: '', allow_insecure_http: true }), "sbm ctl connect --server 'http://192.168.50.142:19090' --allow-insecure-http");
});

test('copied connection values stay literal shell arguments', () => {
  const server = "https://example.invalid/'$(printf INJECTED)";
  const command = agentConnectCommand({ server, fingerprint: '', allow_insecure_http: false });
  const output = execFileSync('/bin/sh', ['-c', `sbm() { printf '%s\\n' "$@"; }; ${command}`], { encoding: 'utf8' });
  assert.ok(output.split('\n').includes(server));
});
