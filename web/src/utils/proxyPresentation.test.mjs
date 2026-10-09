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

test('GLOBAL, Proxy, and managed country groups retain their names and are visible', () => {
  const ctx = { importedTags: new Set(), filters: new Map(), hasImported: false, managedOnly: true };
  for (const tag of ['GLOBAL', 'Proxy', '🇺🇸 美国']) {
    const view = proxyPresentation({tag,type:'Selector',selectable:true,members:['node']},ctx);
    assert.equal(view.name,tag);
    assert.equal(view.auxiliary,false);
  }
  assert.equal(proxyPresentation({tag:'GLOBAL',type:'Fallback',members:['Proxy']},ctx).auxiliary,false);
  assert.equal(proxyPresentation({tag:'DIRECT',type:'Direct'},ctx).name,'DIRECT · 直连');
  assert.equal(proxyPresentation({tag:'REJECT',type:'Reject'},ctx).name,'REJECT · 拒绝');
});
