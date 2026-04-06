Feature: Second Chance card
  A player holding Second Chance is saved from their next bust.
  The bust is cancelled, the SC card is removed from their hand, and their turn
  ends immediately. During Flip 3, the remaining forced draws still continue.
  Each player can hold at most one Second Chance at a time.

  Scenario: Second Chance prevents a bust and ends the player's turn
    Given Alice has [5] and a Second Chance
    And Bob has [7]
    And the deck is [5]
    When Alice draws
    Then Alice is not busted
    And Alice no longer has Second Chance
    And it is Bob's turn

  Scenario: The Second Chance card is removed from Alice's hand after use
    Given Alice has [5] and a Second Chance
    And Bob has [7]
    And the deck is [5]
    When Alice draws
    Then Alice does not have 2nd Chance in her hand

  Scenario: Drawing Second Chance creates a pending target choice
    Given Alice has [3]
    And Bob has [7]
    And the deck is [2nd Chance]
    When Alice draws
    Then a target choice is pending

  Scenario: Alice gives Second Chance to Bob
    Given Alice has [3]
    And Bob has [7]
    And the deck is [2nd Chance]
    When Alice draws
    And Alice targets Bob
    Then Bob has Second Chance
    And no target choice is pending

  Scenario: Alice keeps Second Chance for herself
    Given Alice has [3]
    And Bob has [7]
    And the deck is [2nd Chance]
    When Alice draws
    And Alice targets herself
    Then Alice has Second Chance

  Scenario: When only one valid target exists the card auto-resolves — no choice needed
    Given Alice has [3]
    And Bob has [7] and a Second Chance
    And the deck is [2nd Chance]
    When Alice draws
    Then Alice has Second Chance
    And no target choice is pending

  Scenario: Second Chance saves target mid-Flip 3 and remaining draws continue
    Given Alice has [1]
    And Bob has [5] and a Second Chance
    And the deck is [Flip 3, 3, 5, 9]
    When Alice draws
    And Alice targets Bob
    Then Bob is not busted
    And Bob no longer has Second Chance
    And Bob has 9 in his hand
