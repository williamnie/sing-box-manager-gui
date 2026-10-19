import { useEffect, useRef } from 'react';

// 页面隐藏时暂停轮询，回到前台立即刷新一次；避免后台标签页持续占用路由器 CPU。
export default function useVisibleInterval(callback: () => void, delay: number, enabled = true) {
  const saved = useRef(callback);
  useEffect(() => { saved.current = callback; }, [callback]);

  useEffect(() => {
    if (!enabled) return;
    let timer: number | undefined;
    const start = () => {
      if (timer === undefined && !document.hidden) timer = window.setInterval(() => saved.current(), delay);
    };
    const stop = () => {
      if (timer !== undefined) window.clearInterval(timer);
      timer = undefined;
    };
    const onVisibility = () => {
      if (document.hidden) {
        stop();
      } else {
        saved.current();
        start();
      }
    };
    start();
    document.addEventListener('visibilitychange', onVisibility);
    return () => {
      stop();
      document.removeEventListener('visibilitychange', onVisibility);
    };
  }, [delay, enabled]);
}
