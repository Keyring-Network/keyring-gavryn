import type { Metadata } from 'next'
import { Inter } from 'next/font/google'
import './globals.css'
import Navbar from '@/components/Navbar'
import Footer from '@/components/Footer'

const inter = Inter({ subsets: ['latin'] })

export const metadata: Metadata = {
  title: 'Nottingham Vehicle Recovery | 24/7 Breakdown & Towing Services',
  description: 'Professional vehicle recovery and breakdown services in Nottingham and East Midlands. 24/7 emergency assistance, car towing, and roadside recovery.',
  keywords: 'vehicle recovery Nottingham, car towing East Midlands, breakdown service Nottingham, 24/7 recovery, roadside assistance',
}

export default function RootLayout({
  children,
}: {
  children: React.ReactNode
}) {
  return (
    <html lang="en">
      <body className={inter.className}>
        <Navbar />
        <main>{children}</main>
        <Footer />
      </body>
    </html>
  )
}