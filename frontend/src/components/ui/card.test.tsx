import { describe, it, expect } from 'vitest';
import { render, screen } from '@testing-library/react';
import { Card, CardHeader, CardTitle, CardContent } from './card';
import React from 'react';

describe('Card', () => {
  it('Card renders correctly', () => {
    render(<Card>Card Body</Card>);
    expect(screen.getByText('Card Body')).toHaveClass('rounded-3xl');
  });

  it('CardHeader renders correctly', () => {
    render(<CardHeader>Header</CardHeader>);
    expect(screen.getByText('Header')).toHaveClass('flex flex-col');
  });

  it('CardTitle renders correctly', () => {
    render(<CardTitle>Title</CardTitle>);
    expect(screen.getByRole('heading')).toHaveClass('font-display');
  });

  it('CardContent renders correctly', () => {
    render(<CardContent>Content</CardContent>);
    expect(screen.getByText('Content')).toHaveClass('px-6');
  });

  it('composes together correctly', () => {
    render(
      <Card>
        <CardHeader>
          <CardTitle>Card Title</CardTitle>
        </CardHeader>
        <CardContent>Card Content</CardContent>
      </Card>
    );
    expect(screen.getByText('Card Title')).toBeInTheDocument();
    expect(screen.getByText('Card Content')).toBeInTheDocument();
  });

  it('forwards refs', () => {
    const ref = React.createRef<HTMLDivElement>();
    render(<Card ref={ref}>Card</Card>);
    expect(ref.current).toBeInstanceOf(HTMLDivElement);
  });

  it('accepts custom className', () => {
    render(<Card className="custom-class">Card</Card>);
    expect(screen.getByText('Card')).toHaveClass('custom-class');
  });
});
