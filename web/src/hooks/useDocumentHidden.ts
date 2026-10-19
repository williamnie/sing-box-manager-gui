import { useEffect, useState } from 'react';

// 跟踪标签页是否在后台，供轮询统一暂停。
export default function useDocumentHidden() {
  const [hidden, setHidden] = useState(() => document.hidden);
  useEffect(() => {
    const update = () => setHidden(document.hidden);
    document.addEventListener('visibilitychange', update);
    return () => document.removeEventListener('visibilitychange', update);
  }, []);
  return hidden;
}
