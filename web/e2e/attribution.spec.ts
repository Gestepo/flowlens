import { expect, test } from '@playwright/test'

const pages = [
  ['/', '流量概览'],
  ['/live', '实时流量'],
  ['/domains', '域名分析'],
  ['/owners', '容器与进程'],
  ['/flows', '连接走向'],
  ['/alerts', '异常与告警'],
  ['/settings', '设置'],
] as const

for (const viewport of [{ name: 'desktop', width: 1440, height: 900 }, { name: 'compact-desktop', width: 1024, height: 768 }, { name: 'mobile', width: 390, height: 844 }, { name: 'narrow-mobile', width: 360, height: 800 }]) {
  test(`all dashboard routes work at ${viewport.name}`, async ({ page }) => {
    await page.setViewportSize(viewport)
    for (const [path, heading] of pages) {
      await page.goto(path)
      await expect(page.getByRole('heading', { name: heading })).toBeVisible()
      await expect(page.getByText('正在加载', { exact: true })).toHaveCount(0)
      const overflow = await page.evaluate(() => document.documentElement.scrollWidth - document.documentElement.clientWidth)
      expect(overflow, `${path} must not overflow horizontally`).toBe(0)
      const overlaps = await page.locator('a,button,input,select').evaluateAll((elements) => {
        const visible = elements.filter((element) => { const box = element.getBoundingClientRect(); const style = getComputedStyle(element); return box.width > 2 && box.height > 2 && style.visibility !== 'hidden' && style.display !== 'none' })
        const failures: string[] = []
        for (let first = 0; first < visible.length; first++) for (let second = first + 1; second < visible.length; second++) {
          const a = visible[first], b = visible[second]
          if (a.contains(b) || b.contains(a)) continue
          if (a.closest('.mobile-nav') || b.closest('.mobile-nav')) continue
          const x = a.getBoundingClientRect(), y = b.getBoundingClientRect()
          if (Math.min(x.right, y.right) - Math.max(x.left, y.left) > 2 && Math.min(x.bottom, y.bottom) - Math.max(x.top, y.top) > 2) failures.push(`${a.tagName}.${a.className} overlaps ${b.tagName}.${b.className}`)
        }
        return failures
      })
      expect(overlaps, `${path} interactive controls must not overlap`).toEqual([])
      const pageName = path === '/' ? 'overview' : path.slice(1)
      await page.screenshot({ path: `test-results/route-${pageName}-${viewport.name}.png`, fullPage: true })
    }
  })
}

test('domain direction and filters persist in the URL', async ({ page }) => {
  await page.route('**/api/v1/domains?**', async (route) => {
    const inbound = new URL(route.request().url()).searchParams.get('direction') === 'inbound'
    await route.fulfill({ json: {
      items: inbound
        ? [{ domain: 'monitor.example.com', direction: 'inbound', confidence: 'confirmed', bytes: 1024, connections: 0, requests: 2, owner_count: 1 }]
        : [
            { domain: 'example.com', direction: 'outbound', confidence: 'confirmed', bytes: 2048, connections: 2, requests: 0, owner_count: 1 },
            { domain: 'inferred.example', direction: 'outbound', confidence: 'inferred', bytes: 1024, connections: 1, requests: 0, owner_count: 1 },
            { domain: '203.0.113.10', direction: 'outbound', confidence: 'ip_only', bytes: 512, connections: 1, requests: 0, owner_count: 1 },
          ],
      data_fresh_at: new Date().toISOString(), partial_data: [],
    } })
  })
  await page.goto('/domains?range=7d&direction=inbound')
  await expect(page.getByRole('tab', { name: '入站域名' })).toHaveAttribute('aria-selected', 'true')
  await expect(page.getByText('monitor.example.com').first()).toBeVisible()
  await expect(page.getByText('已确认').first()).toBeVisible()
  await page.getByRole('tab', { name: '出站域名' }).click()
  await expect(page).toHaveURL(/direction=outbound/)
  await expect(page.getByText('example.com').first()).toBeVisible()
  await expect(page.getByText('推断').first()).toBeVisible()
  await expect(page.getByText('仅 IP').first()).toBeVisible()
  await page.getByRole('button', { name: '30 天' }).click()
  await page.getByRole('textbox', { name: '筛选域名' }).fill('example.com')
  await expect(page).toHaveURL(/range=30d/)
  await expect(page).toHaveURL(/direction=outbound/)
  await expect(page).toHaveURL(/domain=example.com/)
  await page.reload()
  await expect(page.getByRole('textbox', { name: '筛选域名' })).toHaveValue('example.com')
})

for (const viewport of [{ name: 'desktop', width: 1440, height: 900 }, { name: 'narrow-mobile', width: 360, height: 800 }]) {
  test(`login renders at ${viewport.name}`, async ({ page }) => {
    await page.context().clearCookies()
    await page.setViewportSize(viewport)
    await page.goto('/')
    await expect(page.getByRole('heading', { name: '登录 FlowLens' })).toBeVisible()
    await expect(page.getByLabel('用户名')).toBeVisible()
    await expect(page.getByRole('textbox', { name: '密码' })).toBeVisible()
    expect(await page.evaluate(() => document.documentElement.scrollWidth - document.documentElement.clientWidth)).toBe(0)
    await page.screenshot({ path: `test-results/login-${viewport.name}.png`, fullPage: true })
  })
}

test('domain tabs render only the requested direction', async ({ page }) => {
  await page.route('**/api/v1/domains?**', async (route) => {
    const inbound = new URL(route.request().url()).searchParams.get('direction') === 'inbound'
    await route.fulfill({ json: {
      items: [{ domain: inbound ? 'inbound-only.test' : 'outbound-only.test', direction: inbound ? 'inbound' : 'outbound', confidence: 'confirmed', bytes: 1024, connections: inbound ? 0 : 1, requests: inbound ? 1 : 0, owner_count: 1 }],
      data_fresh_at: new Date().toISOString(), partial_data: [],
    } })
  })
  await page.goto('/domains?direction=outbound')
  await expect(page.getByText('outbound-only.test')).toBeVisible()
  await expect(page.getByText('inbound-only.test')).toHaveCount(0)
  await page.getByRole('tab', { name: '入站域名' }).click()
  await expect(page.getByText('inbound-only.test')).toBeVisible()
  await expect(page.getByText('outbound-only.test')).toHaveCount(0)
})

test('overview drill-down links open real analysis pages', async ({ page }) => {
  for (const [panelHeading, pageHeading] of [['出站域名排行', '域名分析'], ['容器与进程排行', '容器与进程'], ['主要连接走向', '连接走向']] as const) {
    await page.goto('/')
    const panelHeader = page.getByRole('heading', { name: panelHeading }).locator('..')
    await panelHeader.getByRole('link', { name: '查看全部' }).click()
    await expect(page.getByRole('heading', { name: pageHeading })).toBeVisible()
  }
})

test('live table virtualizes and scrolls through a thousand connections', async ({ page }) => {
  const now = new Date().toISOString()
  await page.route('**/api/v1/live?**', async (route) => {
    await route.fulfill({ json: {
      items: Array.from({ length: 1000 }, (_, index) => ({
        id: `virtual-${index}`, observed_at: now, direction: 'outbound', owner_id: 'container:web', owner_name: 'web',
        source: `10.0.0.2:${42000 + index}`, destination: '203.0.113.10:443', display_name: `virtual-${index}.example.test`,
        confidence: 'confirmed', protocol: 'tcp', state: 'established', bytes_sent: 100, bytes_received: 200,
      })),
      data_fresh_at: now, partial_data: [],
    } })
  })
  await page.goto('/live')
  const viewport = page.getByRole('region', { name: '实时连接列表' })
  await expect(page.getByText('virtual-0.example.test')).toBeVisible()
  expect(await viewport.locator('[data-live-row]').count()).toBeLessThan(40)
  const box = await viewport.boundingBox()
  expect(box?.height).toBeGreaterThanOrEqual(300)
  expect(box?.height).toBeLessThanOrEqual(620)
  await viewport.evaluate((element) => { element.scrollTop = element.scrollHeight })
  await expect(page.getByText('virtual-999.example.test')).toBeVisible()
  expect(await viewport.locator('[data-live-row]').count()).toBeLessThan(40)
})

test('domain drawer and owner detail expose real drill-down data', async ({ page }) => {
  await page.goto('/domains?range=7d&direction=inbound')
  const domainRow = page.locator('button.domain-row:has(.confidence.confirmed)').first()
  await expect(domainRow).toBeVisible()
  const domain = (await domainRow.locator('.rank-cell strong').innerText()).trim()
  await domainRow.click()
  const drawer = page.getByRole('dialog', { name: `${domain} 详情` })
  await expect(drawer.getByRole('heading', { name: 'HTTP 状态' })).toBeVisible()
  await expect(drawer.getByRole('heading', { name: '关联所有者' })).toBeVisible()
  await drawer.getByRole('button', { name: '关闭域名详情' }).click()
  await expect(drawer).toHaveCount(0)

  await page.goto('/owners?range=24h')
  const owner = page.locator('a.owner-row').first()
  await expect(owner).toBeVisible()
  await owner.click()
  await expect(page.getByRole('heading', { name: '流量趋势' })).toBeVisible()
  await expect(page.getByRole('heading', { name: '当前活跃连接' })).toBeVisible()
  await expect(page.getByRole('heading', { name: '监听端口' })).toBeVisible()
})
