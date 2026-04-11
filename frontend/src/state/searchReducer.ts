import type { main } from '../../wailsjs/go/models'

export type ChipDTO = main.ChipDTO

export type SearchPhase = 'idle' | 'typing' | 'grammar_parsed' | 'llm_pending' | 'llm_parsed' | 'relaxed'

export interface SearchState {
  raw: string
  chips: ChipDTO[]
  semanticQuery: string
  chipDenyList: string[]  // clauseKey strings
  phase: SearchPhase
  banner: string | null
}

export type SearchAction =
  | { type: 'KEYSTROKE'; payload: string }
  | { type: 'PARSE_COMPLETE'; payload: { chips: ChipDTO[]; semanticQuery: string } }
  | { type: 'CHIP_REMOVED'; payload: string }
  | { type: 'BANNER_SET'; payload: string }
  | { type: 'CLEAR' }

export function searchReducer(state: SearchState, action: SearchAction): SearchState {
  switch (action.type) {
    case 'KEYSTROKE':
      return { ...state, raw: action.payload, phase: 'typing' }
    case 'PARSE_COMPLETE':
      // Only update chips when NOT in typing phase
      if (state.phase === 'typing') return state
      return {
        ...state,
        chips: action.payload.chips,
        semanticQuery: action.payload.semanticQuery,
        phase: 'llm_parsed',
      }
    case 'CHIP_REMOVED':
      return {
        ...state,
        chips: state.chips.filter(c => c.clauseKey !== action.payload),
        chipDenyList: [...state.chipDenyList, action.payload],
      }
    case 'BANNER_SET':
      return { ...state, banner: action.payload, phase: 'relaxed' }
    case 'CLEAR':
      return { raw: '', chips: [], semanticQuery: '', chipDenyList: [], phase: 'idle', banner: null }
  }
}

export const initialSearchState: SearchState = {
  raw: '',
  chips: [],
  semanticQuery: '',
  chipDenyList: [],
  phase: 'idle',
  banner: null,
}
