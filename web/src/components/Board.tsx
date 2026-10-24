import { FlightScene } from './FlightScene';
import { BoardFrame, type BoardProps } from './BoardFrame';
export function Board(props: BoardProps) {
  return <BoardFrame {...props}><FlightScene {...props} rolling={!!props.rolling} /></BoardFrame>;
}
