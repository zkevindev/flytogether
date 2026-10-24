import { useEffect, useRef } from 'react';

export function focusGame() {
  const target = document.querySelector<HTMLElement>('[data-turn-control]:not(:disabled)') || document.querySelector<HTMLElement>('main');
  target?.focus({ preventScroll: true });
}
export function useDialog() {
  const ref = useRef<HTMLDialogElement>(null);
  useEffect(() => {
    const previous = document.activeElement instanceof HTMLElement ? document.activeElement : null;
    const dialog = ref.current;
    dialog?.showModal();
    const trap = (event: KeyboardEvent) => {
      if (event.key !== 'Tab' || !dialog) return;
      const controls = Array.from(dialog.querySelectorAll<HTMLElement>('button,input,select,textarea,a[href],summary,[tabindex]')).filter(el => el.tabIndex >= 0 && !el.matches(':disabled') && el.getClientRects().length > 0);
      const first = controls[0], last = controls.at(-1);
      if (!first) { event.preventDefault(); dialog.tabIndex = -1; dialog.focus(); return; }
      if (event.shiftKey && (document.activeElement === first || document.activeElement === dialog)) { event.preventDefault(); last?.focus(); }
      else if (!event.shiftKey && document.activeElement === last) { event.preventDefault(); first.focus(); }
    };
    dialog?.addEventListener('keydown', trap);
    return () => {
      dialog?.removeEventListener('keydown', trap);
      requestAnimationFrame(() => {
        if (document.querySelector('dialog[open]')) return;
        const active = document.activeElement;
        // A quick next key may already have focused a preview or another control.
        // Closing the old dialog must not steal that new focus on the next frame.
        if (active && active !== document.body && active !== document.documentElement && active.isConnected) return;
        if (previous?.isConnected && !previous.matches(':disabled')) previous.focus({ preventScroll: true });
        else focusGame();
      });
    };
  }, []);
  return ref;
}
