import type { ReactNode } from 'react';
import type { Game, Move, Piece } from '../lib/types';
import { palette } from '../lib/types';
import { boardPieces, createBoardLayout } from '../lib/board-layout';
import { PlayerMarker } from './PlayerMarker';

export type BoardProps = {
  game: Game; onPiece: (piece: Piece, player: number) => void; disabled: boolean;
  onRoll: () => void; rolling?: boolean; preview?: Move; reducedMotion?: boolean;
};
export function BoardFrame({ children, game, disabled, onPiece, onRoll }: BoardProps & { children: ReactNode }) {
  const canRoll = !disabled && game.phase === 'roll';
  return <div className="flight-board" data-game-id={game.id} data-version={game.version}>
    {children}
    <div className="plane-legend" aria-label="飞机编号对照">{game.players.map((player, index) => <div key={player.id} style={{ color: palette[player.color] }}><strong><PlayerMarker index={index} />{player.name}</strong><span>{player.pieces.map(piece => <span key={piece.id}><b>{piece.number}</b>{piece.label}</span>)}</span></div>)}</div>
    <div className="board-access" role="group" aria-label="棋盘文字操作">
      <button type="button" disabled={!canRoll} onClick={onRoll}>{canRoll ? '掷骰子' : `本次骰子 ${game.dice || '未掷'} 点`}</button>
      {boardPieces(game, createBoardLayout(game.board)).map(({ player, playerIndex, piece }) => {
        const legal = !disabled && game.current === playerIndex && game.legal_moves.some(move => move.piece_id === piece.id);
        const status = piece.progress === game.board.finish ? '已抵达' : piece.progress < 0 ? '在机库' : piece.progress === 0 ? '在起飞位' : `前进到第 ${piece.progress} 格`;
        return <button type="button" key={piece.id} disabled={!legal} onClick={() => onPiece(piece, playerIndex)}>{`${player.name}的${piece.number}号${piece.label}，${status}${piece.shield ? '，护盾保护中' : ''}${legal ? '，可以移动' : ''}`}</button>;
      })}
    </div>
  </div>;
}
