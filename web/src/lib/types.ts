export type Piece = { id: string; label: string; number: number; progress: number };
export type Player = { id: string; name: string; color: string; ai: boolean; pieces: Piece[]; props: Record<string, number> };
export type Move = {
  piece_id: string; label: string; from: number; to: number; captured: string[];
  jump: boolean; fly: boolean; launch: boolean; finish: boolean; summary: string;
};
export type GameEvent = { id: string; kind: string; text: string; player_id?: string; piece_id?: string };
export type BoardSpec = {
  id: string; name: string; layout: 'cross' | 'circle'; ring_length: number;
  home_length: number; finish: number; jump_length: number; fly_from: number; fly_to: number;
};
export type Game = {
  id: string; ruleset: string; board: BoardSpec; mode: string; demo: boolean; version: number; turn: number;
  current: number; phase: 'roll' | 'move' | 'finished'; dice: number; winner?: string;
  players: Player[]; legal_moves: Move[]; history: GameEvent[]; last_move?: Move;
  props_enabled: boolean; active_prop?: string; dice_rolls: number[];
};
export type VoiceSettings = { wake_enabled: boolean; wake_word: string };
export type Room = { id: string; seq: number; state: Game; voice: string; voice_settings: VoiceSettings };
export type Options = { names: string[]; board: string; mode: string; ai: boolean; demo: boolean; props: boolean };
export type Config = { voice_configured: boolean; version: string; ruleset: string; boards: BoardSpec[] };

export class APIError extends Error {
  constructor(public status: number, public code: string, message: string) { super(message); }
}
export async function api<T>(path: string, body?: unknown): Promise<T> {
  const response = await fetch(path, {
    method: body === undefined ? 'GET' : 'POST',
    credentials: 'same-origin',
    headers: body === undefined ? {} : { 'Content-Type': 'application/json' },
    body: body === undefined ? undefined : JSON.stringify(body),
    signal: AbortSignal.timeout(12000),
  });
  const value = await response.json();
  if (!response.ok) throw new APIError(response.status, value.code, value.message || '连接暂时没有成功，请再试一次');
  return value as T;
}
export const socketURL = (path: string) => (location.protocol === 'https:' ? 'wss://' : 'ws://') + location.host + path;
export const colors = ['coral', 'gold', 'blue', 'jade'];
export const palette: Record<string, string> = { coral: '#de594f', gold: '#cd901f', blue: '#387fc0', jade: '#328760' };
