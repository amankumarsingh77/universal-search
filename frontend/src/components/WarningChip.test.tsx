import { describe, it, expect, vi } from 'vitest';
import { render, screen, fireEvent } from '@testing-library/react';
import WarningChip from './WarningChip';

describe('WarningChip', () => {
  it('renders the provided label', () => {
    render(<WarningChip label="Query understanding was slow" />);
    expect(screen.getByText('Query understanding was slow')).toBeInTheDocument();
  });

  it('hides itself when the dismiss button is clicked', () => {
    render(<WarningChip label="Query understanding was slow" />);
    const dismissButton = screen.getByRole('button', { name: /dismiss/i });
    fireEvent.click(dismissButton);
    expect(screen.queryByText('Query understanding was slow')).not.toBeInTheDocument();
  });

  it('calls onDismiss callback when dismiss is clicked', () => {
    const onDismiss = vi.fn();
    render(<WarningChip label="Slow query" onDismiss={onDismiss} />);
    const button = screen.getByRole('button', { name: /dismiss/i });
    fireEvent.click(button);
    expect(onDismiss).toHaveBeenCalledTimes(1);
  });

  it('renders a dismiss button', () => {
    render(<WarningChip label="Something" />);
    expect(screen.getByRole('button', { name: /dismiss/i })).toBeInTheDocument();
  });
});
