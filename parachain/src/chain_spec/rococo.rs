use cumulus_primitives_core::ParaId;
use hex_literal::hex;
use rococo_runtime::{
	GenesisConfig, WASM_BINARY, Signature, AccountId, AuraId
};
use sc_service::{ChainType, Properties};
use sp_core::{sr25519, Pair, Public, U256};
use sp_runtime::{Perbill, traits::{IdentifyAccount, Verify}};

use super::{get_from_seed, Extensions};

/// Specialized `ChainSpec`. This is a specialization of the general Substrate ChainSpec type.
pub type ChainSpec = sc_service::GenericChainSpec<GenesisConfig, Extensions>;

use snowbridge_core::AssetId;

type AccountPublic = <Signature as Verify>::Signer;

/// Helper function to generate an account ID from seed
pub fn get_account_id_from_seed<TPublic: Public>(seed: &str) -> AccountId
where
	AccountPublic: From<<TPublic::Pair as Pair>::Public>,
{
	AccountPublic::from(get_from_seed::<TPublic>(seed)).into_account()
}

pub fn get_chain_spec(para_id: ParaId) -> ChainSpec {
	let mut props = Properties::new();
	props.insert("tokenSymbol".into(), "ROC".into());
	props.insert("tokenDecimals".into(), 12.into());

	ChainSpec::from_genesis(
		"Snowbridge Local Testnet",
		"local_testnet",
		ChainType::Local,
		move || {
			testnet_genesis(
				vec![
					get_from_seed::<AuraId>("Alice"),
					get_from_seed::<AuraId>("Bob"),
				],
				vec![
					get_account_id_from_seed::<sr25519::Public>("Alice"),
					get_account_id_from_seed::<sr25519::Public>("Bob"),
					get_account_id_from_seed::<sr25519::Public>("Charlie"),
					get_account_id_from_seed::<sr25519::Public>("Dave"),
					get_account_id_from_seed::<sr25519::Public>("Eve"),
					get_account_id_from_seed::<sr25519::Public>("Ferdie"),
					get_account_id_from_seed::<sr25519::Public>("Relay"),
					get_account_id_from_seed::<sr25519::Public>("Alice//stash"),
					get_account_id_from_seed::<sr25519::Public>("Bob//stash"),
					get_account_id_from_seed::<sr25519::Public>("Charlie//stash"),
					get_account_id_from_seed::<sr25519::Public>("Dave//stash"),
					get_account_id_from_seed::<sr25519::Public>("Eve//stash"),
					get_account_id_from_seed::<sr25519::Public>("Ferdie//stash"),
				],
				para_id
			)
		},
		vec![],
		None,
		None,
		Some(props),
		Extensions {
			relay_chain: "rococo-local".into(),
			para_id: para_id.into(),
		},
	)
}

/// Configure initial storage state for FRAME modules.
fn testnet_genesis(
	initial_authorities: Vec<AuraId>,
	endowed_accounts: Vec<AccountId>,
	para_id: ParaId
) -> GenesisConfig {
	GenesisConfig {
		system: rococo_runtime::SystemConfig {
			// Add Wasm runtime to storage.
			code: WASM_BINARY
				.expect("WASM binary was not build, please build it!")
				.to_vec(),
			changes_trie_config: Default::default(),
		},
		balances: rococo_runtime::BalancesConfig {
			// Configure endowed accounts with initial balance of 1 << 60.
			balances: endowed_accounts.iter().cloned().map(|k|(k, 1 << 60)).collect(),
		},
		sudo: rococo_runtime::SudoConfig { key: get_account_id_from_seed::<sr25519::Public>("Alice") },
		local_council: Default::default(),
		local_council_membership: rococo_runtime::LocalCouncilMembershipConfig {
			members: vec![
				get_account_id_from_seed::<sr25519::Public>("Alice"),
				get_account_id_from_seed::<sr25519::Public>("Bob"),
				get_account_id_from_seed::<sr25519::Public>("Charlie"),
				get_account_id_from_seed::<sr25519::Public>("Dave"),
			],
			phantom: Default::default()
		},
		basic_inbound_channel: rococo_runtime::BasicInboundChannelConfig {
			source_channel: hex!["B1185EDE04202fE62D38F5db72F71e38Ff3E8305"].into(),
		},
		basic_outbound_channel: rococo_runtime::BasicOutboundChannelConfig {
			principal: get_account_id_from_seed::<sr25519::Public>("Alice"),
			interval: 1,
		},
		incentivized_inbound_channel: rococo_runtime::IncentivizedInboundChannelConfig {
			source_channel: hex!["8cF6147918A5CBb672703F879f385036f8793a24"].into(),
			reward_fraction: Perbill::from_percent(80)
		},
		incentivized_outbound_channel: rococo_runtime::IncentivizedOutboundChannelConfig {
			fee: U256::from_str_radix("10000000000000000", 10).unwrap(), // 0.01 SnowEther
			interval: 1,
		},
		assets: rococo_runtime::AssetsConfig {
			balances: vec![
				(
					AssetId::ETH,
					get_account_id_from_seed::<sr25519::Public>("Ferdie"),
					U256::from_str_radix("1000000000000000000", 10).unwrap()
				)
			]
		},
		nft: rococo_runtime::NFTConfig {
			tokens: vec![]
		},
		ethereum_light_client: rococo_runtime::EthereumLightClientConfig {
			initial_header: Default::default(),
			initial_difficulty: Default::default()
		},
		dot_app: rococo_runtime::DotAppConfig {
			address: hex!["3f839E70117C64744930De8567Ae7A5363487cA3"].into(),
			phantom: Default::default(),
		},
		eth_app: rococo_runtime::EthAppConfig {
			address: hex!["3f0839385DB9cBEa8E73AdA6fa0CFe07E321F61d"].into()
		},
		erc_20_app: rococo_runtime::Erc20AppConfig {
			address: hex!["440eDFFA1352B13227e8eE646f3Ea37456deC701"].into()
		},
		erc_721_app: rococo_runtime::Erc721AppConfig {
			address: hex!["F67EFf5250cD974E6e86c9B53dc5290905Bd8916"].into(),
		},
		parachain_info: rococo_runtime::ParachainInfoConfig { parachain_id: para_id },
		aura: rococo_runtime::AuraConfig {
			authorities: initial_authorities,
		},
		aura_ext: Default::default(),
	}
}
