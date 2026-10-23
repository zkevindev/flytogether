import type { Game, GameEvent } from './types';

export type DiceBeat = { kind: 'dice'; id: string; roll: { id: string; text: string; values: number[] }; events: GameEvent[] };
type BoardBeat = { kind: 'board'; id: string; game: Game; events: GameEvent[]; duration: number };
export type Presentation = {
  sourceID: string;
  seen: string[];
  game?: Game;
  active?: DiceBeat | BoardBeat;
  queue: (DiceBeat | BoardBeat)[];
  effect?: { id: string; events: GameEvent[] };
};
export const initialPresentation: Presentation = { sourceID: '', seen: [], queue: [] };
export type PresentationAction = { type: 'sync'; game: Game; status: string } | { type: 'advance'; id: string };

// Presentation follows server events without delaying or replaying game commands.
// A dice beat keeps the preceding board visible; its result is released on close.
export function presentationReducer(state: Presentation, action: PresentationAction): Presentation {
  if (action.type === 'advance') {
    if (state.active?.id !== action.id) return state;
    return activate({ ...state, active: undefined });
  }
  const { game, status } = action;
  const seen = game.history.map(event => event.id);
  if (state.sourceID !== game.id || status !== 'playing') {
    return { sourceID: game.id, seen, game, queue: [] };
  }
  const fresh = game.history.filter(event => !state.seen.includes(event.id));
  if (!fresh.length) return state;
  const rolls: DiceBeat[] = fresh.filter(event => event.kind === 'rolled' && event.dice_rolls?.length).map(event => ({
    kind: 'dice', id: event.id, roll: { id: event.id, text: event.text, values: event.dice_rolls! }, events: [event],
  }));
  const events = fresh.filter(event => event.kind !== 'rolled');
  const movement = events.some(event => event.kind === 'moved' || (event.kind === 'prop' && event.piece_id));
  const frame: BoardBeat = { kind: 'board', id: `${game.id}:${game.version}`, game, events, duration: movement ? 900 : 0 };
  const next: Presentation = { ...state, seen, queue: [...state.queue, ...rolls, frame] };
  return next.active ? next : activate(next);
}

function activate(state: Presentation): Presentation {
  let game = state.game;
  const events: GameEvent[] = [];
  for (let i = 0; i < state.queue.length; i++) {
    const frame = state.queue[i];
    events.push(...frame.events);
    if (frame.kind === 'board') game = frame.game;
    if (frame.kind === 'dice' || frame.duration > 0) {
      return { ...state, game, active: frame, queue: state.queue.slice(i + 1), effect: { id: frame.id, events } };
    }
  }
  return { ...state, game, active: undefined, queue: [], effect: events.length ? { id: events.at(-1)!.id, events } : state.effect };
}
