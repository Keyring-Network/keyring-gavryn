import { describe, it, expect } from 'vitest';
import { cn } from './utils';

describe('cn', () => {
  it('merges class names correctly', () => {
    expect(cn('foo', 'bar')).toBe('foo bar');
  });

  it('handles conditional classes', () => {
    expect(cn('foo', true && 'bar', false && 'baz')).toBe('foo bar');
  });

  it('removes duplicates with tailwind-merge', () => {
    expect(cn('p-4', 'p-2')).toBe('p-2');
  });

  it('handles empty inputs', () => {
    expect(cn('')).toBe('');
  });

  it('handles undefined/null', () => {
    expect(cn(undefined, null, 'foo')).toBe('foo');
  });
});
