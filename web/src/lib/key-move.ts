import type { Game, GameEvent } from './types';

const keyEvent = (event: GameEvent) => event.kind === 'prop' ||
  (event.kind === 'moved' && /从机场起飞|送\s*\d*\s*架飞机回机场|抵达终点|击破\s*\d*\s*个护盾/.test(event.text));

export function latestKeyMove(game: Pick<Game, 'history'>): GameEvent | undefined {
  return [...game.history].reverse().find(keyEvent);
}
