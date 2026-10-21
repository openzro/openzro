// Device-login approval page (ADR-0028): opened from the openZro app with
// the token in the fragment, no dashboard session. The API is stubbed, so
// the spec needs only the dashboard (`npm run dev`), not a pending login.

const approval = {
  state: "pending",
  hostname: "ana-thinkpad",
  os: "Linux 6.18",
  ip: "203.0.113.7",
  geo_country: "BR",
  geo_city: "São Paulo",
  new_peer: true,
  key_fingerprint: "Oxc1yl…CVQ=",
  masked_email: "a***@example.com",
  account_domain: "example.com",
  created_at: "2026-10-21T12:00:00Z",
  expires_at: "2026-10-21T12:05:00Z",
};

const open = () => cy.visit("/mfa/peer-login#t=page-token");

describe("Device sign-in approval page", () => {
  it("shows the attempt and clears the token from the address bar", () => {
    cy.intercept("GET", "**/api/mfa/peer-login", (req) => {
      expect(req.headers.authorization).to.eq("Bearer page-token");
      req.reply(approval);
    }).as("load");
    open();
    cy.wait("@load");
    cy.get("[data-cy=peer-login-device]")
      .should("contain", "ana-thinkpad")
      .and("contain", "Reported by the device")
      .and("contain", "Observed by openZro")
      .and("contain", "a***@example.com");
    cy.location("hash").should("eq", "");
  });

  it("explains a wrong code and approves with the right one", () => {
    cy.intercept("GET", "**/api/mfa/peer-login", approval).as("load");
    cy.intercept("POST", "**/api/mfa/peer-login/approve", (req) => {
      if (req.body.code === "000000") {
        req.reply(422, { approved: false, reason: "wrong_code" });
      } else {
        req.reply(200, { approved: true });
      }
    }).as("approve");
    open();
    cy.wait("@load");
    cy.get("[data-cy=peer-login-code]").type("000000");
    cy.get("[data-cy=peer-login-approve]").click();
    cy.contains("That code does not match").should("be.visible");

    cy.intercept("GET", "**/api/mfa/peer-login", {
      ...approval,
      state: "approved",
    }).as("reload");
    cy.get("[data-cy=peer-login-code]").clear().type("123456");
    cy.get("[data-cy=peer-login-approve]").click();
    cy.get("[data-cy=peer-login-approved]").should("be.visible");
  });

  it("says when the user is locked out", () => {
    cy.intercept("GET", "**/api/mfa/peer-login", approval);
    cy.intercept("POST", "**/api/mfa/peer-login/approve", {
      statusCode: 423,
      body: {
        approved: false,
        reason: "locked",
        locked_until: "2026-10-21T12:15:00Z",
      },
    });
    open();
    cy.get("[data-cy=peer-login-code]").type("123456");
    cy.get("[data-cy=peer-login-approve]").click();
    cy.contains("Too many wrong codes").should("be.visible");
  });

  it("denies with Not me", () => {
    cy.intercept("GET", "**/api/mfa/peer-login", approval);
    cy.intercept("POST", "**/api/mfa/peer-login/deny", {
      ...approval,
      state: "denied",
    }).as("deny");
    open();
    cy.get("[data-cy=peer-login-deny]").click();
    cy.contains("button", "Deny sign-in").click();
    cy.wait("@deny");
    cy.intercept("GET", "**/api/mfa/peer-login", {
      ...approval,
      state: "denied",
    });
    cy.get("[data-cy=peer-login-denied]").should("be.visible");
  });

  it("handles a link that is not valid or expired", () => {
    cy.intercept("GET", "**/api/mfa/peer-login", { statusCode: 401, body: {} });
    open();
    cy.get("[data-cy=peer-login-invalid]").should("be.visible");

    cy.intercept("GET", "**/api/mfa/peer-login", { statusCode: 412, body: {} });
    cy.visit("/mfa/peer-login#t=other-token");
    cy.get("[data-cy=peer-login-expired]").should("be.visible");
  });

  it("asks for the link again after a reload", () => {
    cy.visit("/mfa/peer-login");
    cy.get("[data-cy=peer-login-no-token]").should("be.visible");
  });
});
