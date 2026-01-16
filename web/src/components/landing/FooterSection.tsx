import { t, Language } from '../../i18n/translations'

interface FooterSectionProps {
  language: Language
}

export default function FooterSection({ language }: FooterSectionProps) {
  const links = {
    supporters: [
      { name: 'Binance', href: 'https://www.binance.com/join?ref=ARESENG' },
      { name: 'Bybit', href: 'https://partner.bybit.com/b/83856' },
      { name: 'OKX', href: 'https://www.okx.com/join/1865360' },
      { name: 'Bitget', href: 'https://www.bitget.com/referral/register?from=referral&clacCode=c8a43172' },
      { name: 'Hyperliquid', href: 'https://app.hyperliquid.xyz/join/AITRADING' },
      { name: 'Aster DEX', href: 'https://www.asterdex.com/en/referral/fdfc0e' },
      { name: 'Lighter', href: 'https://app.lighter.xyz/?referral=68151432' },
    ],
  }

  return (
    <footer style={{ background: '#0B0E11', borderTop: '1px solid rgba(255, 255, 255, 0.06)' }}>
      <div className="max-w-6xl mx-auto px-4 py-8 md:py-12">
        {/* Top Section */}
        <div className="grid grid-cols-1 md:grid-cols-2 gap-8 md:gap-10 mb-8 md:mb-12">
          {/* Brand */}
          <div>
            <div className="flex items-center gap-3 mb-4">
              <img src="/icons/nofx.svg" alt="Ares Logo" className="w-8 h-8" />
              <span className="text-xl font-bold" style={{ color: '#EAECEF' }}>
                Ares
              </span>
            </div>
            <p className="text-sm mb-6" style={{ color: '#5E6673' }}>
              {t('futureStandardAI', language)}
            </p>
          </div>


          {/* Supporters */}
          <div>
            <h4 className="text-sm font-semibold mb-4" style={{ color: '#EAECEF' }}>
              {t('supporters', language)}
            </h4>
            <div className="flex flex-wrap gap-2">
              {links.supporters.map((link) => (
                <a
                  key={link.name}
                  href={link.href}
                  target="_blank"
                  rel="noopener noreferrer"
                  className="text-xs border border-zinc-800 bg-zinc-900/50 rounded px-3 py-1.5 transition-all hover:border-[#F0B90B] hover:text-[#F0B90B] hover:bg-[#F0B90B]/10 hover:shadow-[0_0_10px_rgba(240,185,11,0.2)]"
                  style={{ color: '#848E9C' }}
                >
                  {link.name}
                </a>
              ))}
            </div>
          </div>
        </div>

        {/* Bottom Section */}
        <div
          className="pt-6 text-center text-xs"
          style={{ color: '#5E6673', borderTop: '1px solid rgba(255, 255, 255, 0.06)' }}
        >
          <p className="mb-2">{t('footerTitle', language)}</p>
          <p style={{ color: '#3C4249' }}>{t('footerWarning', language)}</p>
        </div>
      </div>
    </footer>
  )
}
