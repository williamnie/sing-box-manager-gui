import assert from 'node:assert/strict';
import test from 'node:test';
import { logLevel } from './logPresentation.ts';

test('内核启动前 WARN[0000] 格式与带日期级别都可过滤', () => {
  assert.equal(logLevel('WARN[0000] deprecated DNS option'), 'warn');
  assert.equal(logLevel('+0800 2026-09-30 10:25:09 INFO inbound: started'), 'info');
  assert.equal(logLevel('[DEBUG] dns: query'), 'debug');
  assert.equal(logLevel('TRACE: route'), 'trace');
});
test('正文和域名中的 error 不被误判为级别', () => {
  assert.equal(logLevel('connect error.example.com'), 'other');
  assert.equal(logLevel('INFO request returned error'), 'info');
  assert.equal(logLevel('FATAL startup failed'), 'error');
});
