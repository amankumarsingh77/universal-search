import { main } from '../../wailsjs/go/models'

type SearchResultDTO = main.SearchResultDTO
type ChipDTO = main.ChipDTO

function evaluateStringOp(fieldVal: string, op: string, value: string): boolean {
  switch (op) {
    case 'eq':       return fieldVal === value
    case 'neq':      return fieldVal !== value
    case 'contains': return fieldVal.toLowerCase().includes(value.toLowerCase())
    default:         return true // unknown op — don't filter
  }
}

function evaluateNumericOp(fieldVal: number, op: string, threshold: number): boolean {
  switch (op) {
    case 'eq':  return fieldVal === threshold
    case 'neq': return fieldVal !== threshold
    case 'gt':  return fieldVal > threshold
    case 'gte': return fieldVal >= threshold
    case 'lt':  return fieldVal < threshold
    case 'lte': return fieldVal <= threshold
    default:    return true // unknown op — don't filter
  }
}

/**
 * Evaluates a single chip predicate against one result.
 * Returns true if the result satisfies the predicate.
 * must_not chips negate the predicate.
 */
function evaluateChip(result: SearchResultDTO, chip: ChipDTO): boolean {
  const { field, op, value, clauseType } = chip
  const negate = clauseType === 'must_not'

  let passes: boolean

  switch (field) {
    case 'file_type':
      passes = evaluateStringOp(result.fileType, op, value)
      break

    case 'extension':
      if (op === 'in_set') {
        const vals = value.split(',').map(v => v.trim())
        passes = vals.includes(result.extension)
      } else {
        passes = evaluateStringOp(result.extension, op, value)
      }
      break

    case 'size_bytes': {
      const threshold = parseInt(value, 10)
      if (isNaN(threshold)) return true
      passes = evaluateNumericOp(result.sizeBytes, op, threshold)
      break
    }

    case 'modified_at': {
      // Both chip value and result.modifiedAt are Unix seconds
      const threshold = parseInt(value, 10)
      if (isNaN(threshold)) return true
      passes = evaluateNumericOp(result.modifiedAt, op, threshold)
      break
    }

    case 'path':
      passes = evaluateStringOp(result.filePath, op, value)
      break

    default:
      // Unknown or server-only field (semantic_contains) — don't filter
      return true
  }

  return negate ? !passes : passes
}

/**
 * Applies Must/MustNot chips as client-side filters to a result set.
 * Should-clause chips (boosts) are skipped — those only affect server-side ranking.
 * Chips whose clauseKey is in denyList are also skipped.
 * Returns only results that satisfy ALL active chip predicates (AND logic).
 */
export function applyClientSideFilters(
  results: SearchResultDTO[],
  chips: ChipDTO[],
  denyList: string[],
): SearchResultDTO[] {
  const denySet = new Set(denyList)

  const activeChips = chips.filter(
    c => !denySet.has(c.clauseKey) && c.clauseType !== 'should'
  )

  if (activeChips.length === 0) return results

  return results.filter(result =>
    activeChips.every(chip => evaluateChip(result, chip))
  )
}
