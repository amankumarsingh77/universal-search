import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { render, screen, act, fireEvent } from '@testing-library/react';
import ErrorBanner from './ErrorBanner';

describe('ErrorBanner', () => {
  const onRetry = vi.fn();

  beforeEach(() => {
    vi.useFakeTimers();
    onRetry.mockReset();
  });

  afterEach(() => {
    vi.useRealTimers();
  });

  it('renders the label for ERR_QUERY_PARSE_FAILED', () => {
    render(<ErrorBanner code="ERR_QUERY_PARSE_FAILED" retryAfterMs={0} onRetry={onRetry} />);
    expect(screen.getByText('Query understanding failed')).toBeInTheDocument();
  });

  it('renders the description for ERR_QUERY_PARSE_FAILED', () => {
    render(<ErrorBanner code="ERR_QUERY_PARSE_FAILED" retryAfterMs={0} onRetry={onRetry} />);
    expect(screen.getByText(/Gemini could not parse/)).toBeInTheDocument();
  });

  it('renders the label for ERR_QUERY_RATE_LIMITED', () => {
    render(<ErrorBanner code="ERR_QUERY_RATE_LIMITED" retryAfterMs={0} onRetry={onRetry} />);
    expect(screen.getByText('Rate limited')).toBeInTheDocument();
  });

  it('renders the label for ERR_EMBED_FAILED', () => {
    render(<ErrorBanner code="ERR_EMBED_FAILED" retryAfterMs={0} onRetry={onRetry} />);
    expect(screen.getByText('Embedding failed')).toBeInTheDocument();
  });

  it('renders the label for ERR_RATE_LIMITED', () => {
    render(<ErrorBanner code="ERR_RATE_LIMITED" retryAfterMs={0} onRetry={onRetry} />);
    expect(screen.getByText('Rate limited')).toBeInTheDocument();
  });

  it('shows retry button enabled when retryAfterMs is 0', () => {
    render(<ErrorBanner code="ERR_QUERY_PARSE_FAILED" retryAfterMs={0} onRetry={onRetry} />);
    const button = screen.getByRole('button', { name: /retry/i });
    expect(button).not.toBeDisabled();
  });

  it('shows countdown and disables retry button when retryAfterMs > 0', () => {
    render(<ErrorBanner code="ERR_QUERY_RATE_LIMITED" retryAfterMs={5000} onRetry={onRetry} />);
    expect(screen.getByText(/5s/)).toBeInTheDocument();
    const button = screen.getByRole('button', { name: /retry/i });
    expect(button).toBeDisabled();
  });

  it('decrements countdown every second', () => {
    render(<ErrorBanner code="ERR_QUERY_RATE_LIMITED" retryAfterMs={5000} onRetry={onRetry} />);
    expect(screen.getByText(/5s/)).toBeInTheDocument();

    act(() => { vi.advanceTimersByTime(1000); });
    expect(screen.getByText(/4s/)).toBeInTheDocument();

    act(() => { vi.advanceTimersByTime(1000); });
    expect(screen.getByText(/3s/)).toBeInTheDocument();
  });

  it('enables retry button when countdown reaches 0', () => {
    render(<ErrorBanner code="ERR_QUERY_RATE_LIMITED" retryAfterMs={2000} onRetry={onRetry} />);
    const button = screen.getByRole('button', { name: /retry/i });
    expect(button).toBeDisabled();

    act(() => { vi.advanceTimersByTime(2000); });
    expect(button).not.toBeDisabled();
  });

  it('calls onRetry when retry button is clicked', () => {
    render(<ErrorBanner code="ERR_QUERY_PARSE_FAILED" retryAfterMs={0} onRetry={onRetry} />);
    const button = screen.getByRole('button', { name: /retry/i });
    fireEvent.click(button);
    expect(onRetry).toHaveBeenCalledTimes(1);
  });

  it('does not call onRetry when button is disabled during countdown', () => {
    render(<ErrorBanner code="ERR_QUERY_RATE_LIMITED" retryAfterMs={5000} onRetry={onRetry} />);
    const button = screen.getByRole('button', { name: /retry/i });
    fireEvent.click(button);
    expect(onRetry).not.toHaveBeenCalled();
  });
});
