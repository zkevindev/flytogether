import type { Game } from '../lib/types';
import { latestKeyMove } from '../lib/key-move';

export function KeyMoveNarration({ game }: { game: Game }) {
  const event = latestKeyMove(game);
  if (!event) return null;

  return <section className="key-move-narration" aria-label="刚刚发生的棋步" role="status" aria-live="polite" aria-atomic="true">
    <span>刚刚发生</span>
    <p>{event.text}</p>
  </section>;
}
