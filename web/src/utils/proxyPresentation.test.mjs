import test from 'node:test';
import assert from 'node:assert/strict';
import { proxyPresentation } from './proxyPresentation.ts';
const context = { importedTags: new Set(['SMbox/自建家宽', 'Managed Auto']), filters: new Map([['家庭代理', 'family']]), hasImported: true };
test('classifies by known provenance before reserved labels', () => {
  assert.equal(proxyPresentation({tag:'Managed Auto'},context).source,'导入配置');
  assert.equal(proxyPresentation({tag:'Managed Proxy'},context).name,'订阅节点选择');
  assert.equal(proxyPresentation({tag:'家庭代理'},context).auxiliary,false);
  assert.equal(proxyPresentation({tag:'SMbox/自建家宽'},context).name,'SMbox/自建家宽');
  assert.equal(proxyPresentation({tag:'陌生节点',type:'Selector'},context).source,'运行配置');
});

test('different imported tags must not become identically named groups', () => {
  const ctx = {...context, importedTags:new Set(['Proxy','SMbox/Proxy'])};
  assert.notEqual(proxyPresentation({tag:'Proxy'},ctx).name,proxyPresentation({tag:'SMbox/Proxy'},ctx).name);
});
