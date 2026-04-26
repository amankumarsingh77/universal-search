export const CODE_LABELS: Record<string, string> = {
  ERR_UNSUPPORTED_FORMAT: "Unsupported format",
  ERR_EXTRACTION_FAILED: "Extraction failed",
  ERR_FILE_TOO_LARGE: "File too large",
  ERR_FILE_UNREADABLE: "File unreadable",
  ERR_EMBED_FAILED: "Embedding failed",
  ERR_EMBED_COUNT_MISMATCH: "Embedding count mismatch",
  ERR_RATE_LIMITED: "Rate limited",
  ERR_HNSW_ADD: "Index write failed",
  ERR_STORE_WRITE: "Database write failed",
  ERR_INTERNAL: "Internal error",
  ERR_QUERY_PARSE_FAILED: "Query understanding failed",
  ERR_QUERY_RATE_LIMITED: "Rate limited",
  ERR_FILENAME_SEARCH_FAILED: "Filename search failed",
  ERR_CLASSIFIER_FAILED: "Query classification failed",
};

export const CODE_DESCRIPTIONS: Record<string, string> = {
  ERR_EXTRACTION_FAILED: "Couldn't read the document's content. Often scanned PDFs, corrupt files, or unsupported legacy Office formats missing LibreOffice.",
  ERR_FILE_TOO_LARGE: "File exceeds the indexer's size limit.",
  ERR_UNSUPPORTED_FORMAT: "File type is not supported for indexing.",
  ERR_FILE_UNREADABLE: "Couldn't open or stat the file — often a permissions issue.",
  ERR_RATE_LIMITED: "The embedding API is rate-limiting requests. These will retry automatically.",
  ERR_EMBED_FAILED: "The embedding API returned an error.",
  ERR_EMBED_COUNT_MISMATCH: "The embedder returned a different number of vectors than requested.",
  ERR_HNSW_ADD: "Writing the vector into the search index failed.",
  ERR_STORE_WRITE: "Writing to the local database failed.",
  ERR_INTERNAL: "An unexpected error occurred.",
  ERR_QUERY_PARSE_FAILED: "Gemini could not parse your query. Check your connection and try again.",
  ERR_QUERY_RATE_LIMITED: "Too many requests to Gemini. Retrying is throttled.",
  ERR_FILENAME_SEARCH_FAILED: "Filename search failed. Try again.",
  ERR_CLASSIFIER_FAILED: "Could not understand the query. Try simpler terms.",
};

export function labelForCode(code: string, fallback: string): string {
  return CODE_LABELS[code] ?? fallback ?? code;
}

export function descriptionForCode(code: string): string | null {
  return CODE_DESCRIPTIONS[code] ?? null;
}
