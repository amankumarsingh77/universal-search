import { describe, it, expect } from 'vitest';
import { labelForCode, descriptionForCode, CODE_LABELS, CODE_DESCRIPTIONS } from './errorLabels';

describe('errorLabels', () => {
  describe('ERR_QUERY_PARSE_FAILED', () => {
    it('has the label "Query understanding failed"', () => {
      expect(CODE_LABELS['ERR_QUERY_PARSE_FAILED']).toBe('Query understanding failed');
    });

    it('has a description mentioning Gemini and connection', () => {
      expect(CODE_DESCRIPTIONS['ERR_QUERY_PARSE_FAILED']).toContain('Gemini');
    });

    it('labelForCode returns the label', () => {
      expect(labelForCode('ERR_QUERY_PARSE_FAILED', 'fallback')).toBe('Query understanding failed');
    });

    it('descriptionForCode returns non-null', () => {
      expect(descriptionForCode('ERR_QUERY_PARSE_FAILED')).not.toBeNull();
    });
  });

  describe('ERR_QUERY_RATE_LIMITED', () => {
    it('has the label "Rate limited"', () => {
      expect(CODE_LABELS['ERR_QUERY_RATE_LIMITED']).toBe('Rate limited');
    });

    it('has a description mentioning throttle/rate', () => {
      const desc = CODE_DESCRIPTIONS['ERR_QUERY_RATE_LIMITED'];
      expect(desc).toBeTruthy();
      expect(desc?.toLowerCase()).toMatch(/rate|throttl/);
    });

    it('labelForCode returns the label', () => {
      expect(labelForCode('ERR_QUERY_RATE_LIMITED', 'fallback')).toBe('Rate limited');
    });

    it('descriptionForCode returns non-null', () => {
      expect(descriptionForCode('ERR_QUERY_RATE_LIMITED')).not.toBeNull();
    });
  });
});
